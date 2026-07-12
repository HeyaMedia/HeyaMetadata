package cmd

import (
	"fmt"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/jobs"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/HeyaMedia/HeyaMetadata/internal/ui"
	"github.com/riverqueue/river/rivertype"
	"github.com/spf13/cobra"
)

func newRecordingCommand() *cobra.Command {
	command := &cobra.Command{Use: "recording", Short: "Manage canonical music recordings", Args: cobra.NoArgs}
	command.AddCommand(newRecordingIngestCommand())
	return command
}

func newRecordingIngestCommand() *cobra.Command {
	var mbid string
	var wait time.Duration
	command := &cobra.Command{Use: "ingest", Short: "Ingest a standalone MusicBrainz recording", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		runtime, err := platform.Open(cmd.Context(), cfg)
		if err != nil {
			return err
		}
		defer runtime.Close()
		if err := runtime.Ensure(cmd.Context(), cfg); err != nil {
			return err
		}
		if err := requireCurrentSchema(cmd.Context(), runtime); err != nil {
			return err
		}
		client, err := jobs.NewClient(runtime, cfg.Worker.MaxWorkers, false)
		if err != nil {
			return err
		}
		inserted, err := jobs.InsertRecording(cmd.Context(), runtime, client, jobs.RecordingIngestArgs{MusicBrainzID: mbid, Reason: "cli"}, jobs.PriorityInteractive)
		if err != nil {
			return err
		}
		if wait <= 0 {
			if ui.JSONMode {
				return ui.OutputJSON(map[string]any{"job_id": inserted.Job.ID, "state": "queued", "musicbrainz_id": mbid})
			}
			ui.Success("Queued recording %s as job %d", mbid, inserted.Job.ID)
			return nil
		}
		deadline := time.NewTimer(wait)
		defer deadline.Stop()
		ticker := time.NewTicker(150 * time.Millisecond)
		defer ticker.Stop()
		for {
			var state string
			var entityID, failure *string
			if err := runtime.DB.QueryRow(cmd.Context(), `SELECT state,entity_id,error FROM recording_ingestion_runs WHERE river_job_id=$1`, inserted.Job.ID).Scan(&state, &entityID, &failure); err == nil && state == "completed" {
				if ui.JSONMode {
					return ui.OutputJSON(map[string]any{"job_id": inserted.Job.ID, "state": state, "entity_id": entityID, "musicbrainz_id": mbid})
				}
				ui.Success("Recording ingested")
				ui.Info("Entity", *entityID)
				return nil
			}
			var riverState rivertype.JobState
			if err := runtime.DB.QueryRow(cmd.Context(), `SELECT state FROM river_job WHERE id=$1`, inserted.Job.ID).Scan(&riverState); err == nil && (riverState == rivertype.JobStateCancelled || riverState == rivertype.JobStateDiscarded) {
				if failure != nil {
					return fmt.Errorf("recording ingestion failed: %s", *failure)
				}
				return fmt.Errorf("job ended in state %s", riverState)
			}
			select {
			case <-cmd.Context().Done():
				return cmd.Context().Err()
			case <-deadline.C:
				return fmt.Errorf("timed out waiting for job %d", inserted.Job.ID)
			case <-ticker.C:
			}
		}
	}}
	command.Flags().StringVar(&mbid, "musicbrainz", "", "MusicBrainz recording MBID")
	command.Flags().DurationVar(&wait, "wait", 90*time.Second, "Wait for completion (0 disables waiting)")
	_ = command.MarkFlagRequired("musicbrainz")
	return command
}

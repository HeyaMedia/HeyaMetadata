package cmd

import (
	"fmt"
	"strconv"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/jobs"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/HeyaMedia/HeyaMetadata/internal/ui"
	"github.com/riverqueue/river/rivertype"
	"github.com/spf13/cobra"
)

func newMusicalWorkCommand() *cobra.Command {
	command := &cobra.Command{Use: "musical-work", Short: "Manage canonical composed musical works", Args: cobra.NoArgs}
	command.AddCommand(newMusicalWorkIngestCommand())
	return command
}

func newMusicalWorkIngestCommand() *cobra.Command {
	var openOpusID string
	var wait time.Duration
	command := &cobra.Command{Use: "ingest", Short: "Ingest a canonical musical work from Open Opus", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if id, err := strconv.ParseInt(openOpusID, 10, 64); err != nil || id < 1 {
			return fmt.Errorf("--openopus must be a positive work ID")
		}
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
		inserted, err := jobs.InsertMusicalWork(cmd.Context(), runtime, client, jobs.MusicalWorkIngestArgs{OpenOpusWorkID: openOpusID, Reason: "cli"}, jobs.PriorityInteractive)
		if err != nil {
			return err
		}
		if wait <= 0 {
			if ui.JSONMode {
				return ui.OutputJSON(map[string]any{"job_id": inserted.Job.ID, "state": "queued", "openopus_work_id": openOpusID})
			}
			ui.Success("Queued Open Opus work %s as job %d", openOpusID, inserted.Job.ID)
			return nil
		}
		deadline := time.NewTimer(wait)
		defer deadline.Stop()
		ticker := time.NewTicker(150 * time.Millisecond)
		defer ticker.Stop()
		for {
			var state string
			var entityID, failure *string
			if err := runtime.DB.QueryRow(cmd.Context(), `SELECT state,entity_id,error FROM musical_work_ingestion_runs WHERE river_job_id=$1`, inserted.Job.ID).Scan(&state, &entityID, &failure); err == nil {
				switch state {
				case "completed":
					if ui.JSONMode {
						return ui.OutputJSON(map[string]any{"job_id": inserted.Job.ID, "state": state, "entity_id": entityID, "openopus_work_id": openOpusID})
					}
					ui.Success("Open Opus work %s ingested", openOpusID)
					ui.Info("Entity", *entityID)
					return nil
				case "failed":
					if failure != nil {
						return fmt.Errorf("musical-work ingestion failed: %s", *failure)
					}
					return fmt.Errorf("musical-work ingestion failed")
				}
			}
			var riverState rivertype.JobState
			if err := runtime.DB.QueryRow(cmd.Context(), `SELECT state FROM river_job WHERE id=$1`, inserted.Job.ID).Scan(&riverState); err == nil && (riverState == rivertype.JobStateCancelled || riverState == rivertype.JobStateDiscarded) {
				if failure != nil {
					return fmt.Errorf("musical-work ingestion failed: %s", *failure)
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
	command.Flags().StringVar(&openOpusID, "openopus", "", "Open Opus work ID")
	command.Flags().DurationVar(&wait, "wait", 90*time.Second, "Wait for completion (0 disables waiting)")
	_ = command.MarkFlagRequired("openopus")
	return command
}

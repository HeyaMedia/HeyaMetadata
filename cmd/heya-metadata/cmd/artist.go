package cmd

import (
	"fmt"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/jobs"
	"github.com/HeyaMedia/HeyaMetadata/internal/musiccatalog"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/HeyaMedia/HeyaMetadata/internal/ui"
	"github.com/riverqueue/river/rivertype"
	"github.com/spf13/cobra"
)

func newArtistCommand() *cobra.Command {
	command := &cobra.Command{Use: "artist", Short: "Manage canonical artists", Args: cobra.NoArgs}
	command.AddCommand(newArtistIngestCommand())
	command.AddCommand(newArtistCatalogAuditCommand())
	return command
}

func newArtistCatalogAuditCommand() *cobra.Command {
	var entityID string
	command := &cobra.Command{Use: "catalog-audit", Short: "Inspect mixed-discography confidence, conflicts, and possible duplicates", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if entityID == "" {
			return fmt.Errorf("--id is required")
		}
		runtime, err := platform.Open(cmd.Context(), cfg)
		if err != nil {
			return err
		}
		defer runtime.Close()
		if err := runtime.Ensure(cmd.Context(), cfg); err != nil {
			return err
		}
		report, err := musiccatalog.AuditArtist(cmd.Context(), runtime, entityID)
		if err != nil {
			return err
		}
		if ui.JSONMode {
			return ui.OutputJSON(report)
		}
		ui.Success("Catalog audit for %s", report.ArtistName)
		ui.Info("Clusters", fmt.Sprintf("%d", report.Clusters))
		ui.Info("Canonical", fmt.Sprintf("%d", report.CanonicalTargets))
		ui.Info("Candidates", fmt.Sprintf("%d", report.CandidateOnly))
		ui.Info("Unresolved", fmt.Sprintf("%d", report.Unresolved))
		ui.Info("Weak", fmt.Sprintf("%d", report.Weak))
		ui.Info("Conflicts", fmt.Sprintf("%d", report.Conflicts))
		ui.Info("Possible dupes", fmt.Sprintf("%d", len(report.PotentialDupes)))
		ui.Info("Strong bridges", fmt.Sprintf("%d", report.StrongBridges))
		return nil
	}}
	command.Flags().StringVar(&entityID, "id", "", "Canonical artist entity ID")
	_ = command.MarkFlagRequired("id")
	return command
}
func newArtistIngestCommand() *cobra.Command {
	var mbid string
	var wait time.Duration
	command := &cobra.Command{Use: "ingest", Short: "Ingest and combine an artist through the durable provider pipeline", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if mbid == "" {
			return fmt.Errorf("--musicbrainz is required")
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
		inserted, err := jobs.InsertArtist(cmd.Context(), runtime, client, jobs.ArtistIngestArgs{MusicBrainzID: mbid, Reason: "cli"}, jobs.PriorityInteractive)
		if err != nil {
			return fmt.Errorf("enqueue MusicBrainz artist ingestion: %w", err)
		}
		if wait <= 0 {
			return outputArtistQueued(inserted.Job.ID, mbid)
		}
		return waitForArtist(cmd, runtime, inserted.Job.ID, mbid, wait)
	}}
	command.Flags().StringVar(&mbid, "musicbrainz", "", "MusicBrainz artist MBID")
	command.Flags().DurationVar(&wait, "wait", 60*time.Second, "Wait for completion (0 disables waiting)")
	_ = command.MarkFlagRequired("musicbrainz")
	return command
}
func waitForArtist(cmd *cobra.Command, runtime *platform.Runtime, jobID int64, mbid string, wait time.Duration) error {
	deadline := time.NewTimer(wait)
	defer deadline.Stop()
	ticker := time.NewTicker(150 * time.Millisecond)
	defer ticker.Stop()
	for {
		var state string
		var entityID, failure *string
		err := runtime.DB.QueryRow(cmd.Context(), `SELECT state,entity_id,error FROM artist_ingestion_runs WHERE river_job_id=$1`, jobID).Scan(&state, &entityID, &failure)
		if err == nil {
			switch state {
			case "completed":
				if ui.JSONMode {
					return ui.OutputJSON(map[string]any{"job_id": jobID, "state": state, "entity_id": entityID, "musicbrainz_id": mbid})
				}
				ui.Success("MusicBrainz artist %s ingested", mbid)
				ui.Info("Entity", *entityID)
				ui.Info("Job", fmt.Sprintf("%d", jobID))
				return nil
			case "failed":
				if failure != nil {
					return fmt.Errorf("artist ingestion failed: %s", *failure)
				}
				return fmt.Errorf("artist ingestion failed")
			}
		}
		var riverState rivertype.JobState
		if stateErr := runtime.DB.QueryRow(cmd.Context(), `SELECT state FROM river_job WHERE id=$1`, jobID).Scan(&riverState); stateErr == nil && (riverState == rivertype.JobStateCancelled || riverState == rivertype.JobStateDiscarded) {
			return fmt.Errorf("artist ingestion job %d ended in state %s", jobID, riverState)
		}
		select {
		case <-cmd.Context().Done():
			return cmd.Context().Err()
		case <-deadline.C:
			return fmt.Errorf("timed out after %s waiting for artist ingestion job %d", wait, jobID)
		case <-ticker.C:
		}
	}
}
func outputArtistQueued(jobID int64, mbid string) error {
	if ui.JSONMode {
		return ui.OutputJSON(map[string]any{"job_id": jobID, "state": "queued", "musicbrainz_id": mbid})
	}
	ui.Success("Queued MusicBrainz artist %s as job %d", mbid, jobID)
	return nil
}

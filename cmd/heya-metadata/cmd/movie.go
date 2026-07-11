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

func newMovieCommand() *cobra.Command {
	command := &cobra.Command{Use: "movie", Short: "Manage canonical movies", Args: cobra.NoArgs}
	command.AddCommand(newMovieIngestCommand())
	return command
}

func newMovieIngestCommand() *cobra.Command {
	var tmdbID int64
	var wait time.Duration
	command := &cobra.Command{
		Use:   "ingest",
		Short: "Ingest and combine a movie through the durable provider pipeline",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if tmdbID < 1 {
				return fmt.Errorf("--tmdb must be a positive TMDB movie ID")
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
			inserted, err := jobs.InsertMovie(cmd.Context(), runtime, client, jobs.MovieIngestArgs{TMDBID: tmdbID, Reason: "cli"}, jobs.PriorityInteractive)
			if err != nil {
				return fmt.Errorf("enqueue TMDB movie ingestion: %w", err)
			}
			if wait <= 0 {
				return outputMovieQueued(inserted.Job.ID, tmdbID)
			}
			return waitForMovie(cmd, runtime, inserted.Job.ID, tmdbID, wait)
		},
	}
	command.Flags().Int64Var(&tmdbID, "tmdb", 0, "TMDB movie ID")
	command.Flags().DurationVar(&wait, "wait", 60*time.Second, "Wait for completion (0 disables waiting)")
	_ = command.MarkFlagRequired("tmdb")
	return command
}

func waitForMovie(cmd *cobra.Command, runtime *platform.Runtime, jobID, tmdbID int64, wait time.Duration) error {
	deadline := time.NewTimer(wait)
	defer deadline.Stop()
	ticker := time.NewTicker(150 * time.Millisecond)
	defer ticker.Stop()
	for {
		var state string
		var entityID *string
		var failure *string
		err := runtime.DB.QueryRow(cmd.Context(), `SELECT state, entity_id, error FROM movie_ingestion_runs WHERE river_job_id = $1`, jobID).Scan(&state, &entityID, &failure)
		if err == nil {
			switch state {
			case "completed":
				if ui.JSONMode {
					return ui.OutputJSON(map[string]any{"job_id": jobID, "state": state, "entity_id": entityID, "tmdb_id": tmdbID})
				}
				ui.Success("TMDB movie %d ingested", tmdbID)
				ui.Info("Entity", *entityID)
				ui.Info("Job", fmt.Sprintf("%d", jobID))
				return nil
			case "failed":
				if failure != nil {
					return fmt.Errorf("movie ingestion failed: %s", *failure)
				}
				return fmt.Errorf("movie ingestion failed")
			}
		}
		var riverState rivertype.JobState
		if stateErr := runtime.DB.QueryRow(cmd.Context(), `SELECT state FROM river_job WHERE id = $1`, jobID).Scan(&riverState); stateErr == nil {
			if riverState == rivertype.JobStateCancelled || riverState == rivertype.JobStateDiscarded {
				return fmt.Errorf("movie ingestion job %d ended in state %s", jobID, riverState)
			}
		}
		select {
		case <-cmd.Context().Done():
			return cmd.Context().Err()
		case <-deadline.C:
			return fmt.Errorf("timed out after %s waiting for movie ingestion job %d", wait, jobID)
		case <-ticker.C:
		}
	}
}

func outputMovieQueued(jobID, tmdbID int64) error {
	if ui.JSONMode {
		return ui.OutputJSON(map[string]any{"job_id": jobID, "state": "queued", "tmdb_id": tmdbID})
	}
	ui.Success("Queued TMDB movie %d as job %d", tmdbID, jobID)
	return nil
}

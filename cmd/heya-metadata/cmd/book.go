package cmd

import (
	"fmt"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/jobs"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/HeyaMedia/HeyaMetadata/internal/providercredentials"
	"github.com/HeyaMedia/HeyaMetadata/internal/ui"
	"github.com/spf13/cobra"
)

func newBookCommand() *cobra.Command {
	command := &cobra.Command{Use: "book", Short: "Manage canonical books", Args: cobra.NoArgs}
	var workID, apiKey string
	var wait time.Duration
	ingest := &cobra.Command{Use: "ingest", Short: "Ingest an Open Library work and its editions", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		runtime, err := platform.Open(cmd.Context(), cfg)
		if err != nil {
			return err
		}
		defer runtime.Close()
		if err = runtime.Ensure(cmd.Context(), cfg); err != nil {
			return err
		}
		if err = requireCurrentSchema(cmd.Context(), runtime); err != nil {
			return err
		}
		credentials := providercredentials.Credentials{}
		if apiKey != "" {
			credentials.APIKeys = map[string]string{"googlebooks": apiKey}
		}
		ref, err := providercredentials.Store(cmd.Context(), runtime.Redis, credentials)
		if err != nil {
			return err
		}
		client, err := jobs.NewClient(runtime, cfg.Worker.MaxWorkers, false)
		if err != nil {
			return err
		}
		inserted, err := jobs.InsertBook(cmd.Context(), runtime, client, jobs.BookIngestArgs{OpenLibraryWorkID: workID, CredentialRef: ref, Reason: "cli"}, jobs.PriorityInteractive)
		if err != nil {
			return err
		}
		ui.Success("Queued Open Library work %s as job %d", workID, inserted.Job.ID)
		deadline := time.Now().Add(wait)
		for time.Now().Before(deadline) {
			var state string
			var entityID, failure *string
			if err = runtime.DB.QueryRow(cmd.Context(), `SELECT state,entity_id,error FROM book_ingestion_runs WHERE river_job_id=$1`, inserted.Job.ID).Scan(&state, &entityID, &failure); err == nil && (state == "completed" || state == "failed") {
				if state == "failed" {
					return fmt.Errorf("book ingestion failed: %s", valueOrEmpty(failure))
				}
				ui.Success("Book work ingested as %s", valueOrEmpty(entityID))
				return nil
			}
			time.Sleep(200 * time.Millisecond)
		}
		return fmt.Errorf("timed out waiting for book ingestion job %d", inserted.Job.ID)
	}}
	ingest.Flags().StringVar(&workID, "openlibrary", "", "Open Library work key (for example OL45883W)")
	ingest.Flags().StringVar(&apiKey, "google-books-api-key", "", "Request-scoped Google Books API key")
	ingest.Flags().DurationVar(&wait, "wait", 2*time.Minute, "Maximum wait for ingestion")
	_ = ingest.MarkFlagRequired("openlibrary")
	command.AddCommand(ingest)
	return command
}
func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

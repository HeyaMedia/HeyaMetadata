package cmd

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/artists"
	"github.com/HeyaMedia/HeyaMetadata/internal/jobs"
	"github.com/HeyaMedia/HeyaMetadata/internal/musiccatalog"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/HeyaMedia/HeyaMetadata/internal/ui"
	"github.com/google/uuid"
	"github.com/riverqueue/river/rivertype"
	"github.com/spf13/cobra"
)

func newArtistCommand() *cobra.Command {
	command := &cobra.Command{Use: "artist", Short: "Manage canonical artists", Args: cobra.NoArgs}
	command.AddCommand(newArtistIngestCommand())
	command.AddCommand(newArtistUpdateCommand())
	command.AddCommand(newArtistCatalogAuditCommand())
	return command
}

func newArtistUpdateCommand() *cobra.Command {
	var wait time.Duration
	command := &cobra.Command{
		Use:   "update <Heya artist URL or UUID>",
		Short: "Update an existing canonical artist through the durable provider pipeline",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			requestedID, err := parseArtistReference(args[0], cfg.SiteURL)
			if err != nil {
				return err
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

			service := artists.NewService(runtime)
			entityID, err := service.CanonicalID(cmd.Context(), requestedID)
			if err != nil {
				return fmt.Errorf("resolve Heya artist %s: %w", requestedID, err)
			}
			provider, providerID, err := service.RefreshRoot(cmd.Context(), entityID)
			if err != nil {
				return fmt.Errorf("artist %s has no refreshable identity: %w", entityID, err)
			}
			client, err := jobs.NewClient(runtime, cfg.Worker.MaxWorkers, false)
			if err != nil {
				return err
			}
			inserted, err := jobs.InsertArtist(cmd.Context(), runtime, client, jobs.ArtistIngestArgs{Provider: provider, ProviderID: providerID, Reason: "cli_update"}, jobs.PriorityInteractive)
			if err != nil {
				return fmt.Errorf("enqueue artist %s update: %w", entityID, err)
			}
			if wait <= 0 {
				return outputArtistUpdateQueued(inserted.Job.ID, entityID)
			}
			return waitForArtistUpdate(cmd, runtime, inserted.Job.ID, requestedID, wait)
		},
	}
	command.Flags().DurationVar(&wait, "wait", 5*time.Minute, "Wait for the artist update (0 disables waiting)")
	return command
}

func parseArtistReference(raw, siteURL string) (string, error) {
	value := strings.TrimSpace(raw)
	if id, err := uuid.Parse(value); err == nil {
		return id.String(), nil
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.User != nil {
		return "", fmt.Errorf("artist reference must be a Heya artist URL or UUID")
	}
	allowedHosts := map[string]bool{"heya.media": true}
	if site, siteErr := url.Parse(siteURL); siteErr == nil && site.Hostname() != "" {
		allowedHosts[strings.ToLower(site.Hostname())] = true
	}
	if !allowedHosts[strings.ToLower(parsed.Hostname())] {
		return "", fmt.Errorf("artist URL host %q is not the configured Heya site", parsed.Hostname())
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) != 2 || parts[0] != "artists" {
		return "", fmt.Errorf("artist URL must use /artists/{heya-id}")
	}
	id, err := uuid.Parse(parts[1])
	if err != nil {
		return "", fmt.Errorf("artist URL contains an invalid Heya ID: %w", err)
	}
	return id.String(), nil
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
	var mbid, appleID, deezerID string
	var wait time.Duration
	command := &cobra.Command{Use: "ingest", Short: "Ingest and combine an artist through the durable provider pipeline", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		provider, providerID := "", ""
		for _, value := range []struct{ provider, id string }{{"musicbrainz", mbid}, {"apple", appleID}, {"deezer", deezerID}} {
			if value.id == "" {
				continue
			}
			if provider != "" {
				return fmt.Errorf("exactly one of --musicbrainz, --apple, or --deezer is required")
			}
			provider, providerID = value.provider, value.id
		}
		if provider == "" {
			return fmt.Errorf("exactly one of --musicbrainz, --apple, or --deezer is required")
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
		inserted, err := jobs.InsertArtist(cmd.Context(), runtime, client, jobs.ArtistIngestArgs{Provider: provider, ProviderID: providerID, Reason: "cli"}, jobs.PriorityInteractive)
		if err != nil {
			return fmt.Errorf("enqueue %s artist ingestion: %w", provider, err)
		}
		if wait <= 0 {
			return outputArtistQueued(inserted.Job.ID, provider, providerID)
		}
		return waitForArtist(cmd, runtime, inserted.Job.ID, provider, providerID, wait)
	}}
	command.Flags().StringVar(&mbid, "musicbrainz", "", "MusicBrainz artist MBID")
	command.Flags().StringVar(&appleID, "apple", "", "Apple/iTunes artist ID")
	command.Flags().StringVar(&deezerID, "deezer", "", "Deezer artist ID")
	command.Flags().DurationVar(&wait, "wait", 60*time.Second, "Wait for completion (0 disables waiting)")
	return command
}
func waitForArtist(cmd *cobra.Command, runtime *platform.Runtime, jobID int64, provider, providerID string, wait time.Duration) error {
	entityID, err := waitForArtistResult(cmd, runtime, jobID, wait)
	if err != nil {
		return err
	}
	if ui.JSONMode {
		return ui.OutputJSON(map[string]any{"job_id": jobID, "state": "completed", "entity_id": entityID, "provider": provider, "provider_id": providerID})
	}
	ui.Success("%s artist %s ingested", provider, providerID)
	ui.Info("Entity", entityID)
	ui.Info("Job", fmt.Sprintf("%d", jobID))
	return nil
}

func waitForArtistUpdate(cmd *cobra.Command, runtime *platform.Runtime, jobID int64, requestedID string, wait time.Duration) error {
	entityID, err := waitForArtistResult(cmd, runtime, jobID, wait)
	if err != nil {
		return err
	}
	if ui.JSONMode {
		return ui.OutputJSON(map[string]any{"job_id": jobID, "state": "completed", "entity_id": entityID, "requested_entity_id": requestedID})
	}
	ui.Success("Artist updated")
	ui.Info("Entity", entityID)
	if requestedID != entityID {
		ui.Info("Redirected from", requestedID)
	}
	ui.Info("Job", fmt.Sprintf("%d", jobID))
	return nil
}

func waitForArtistResult(cmd *cobra.Command, runtime *platform.Runtime, jobID int64, wait time.Duration) (string, error) {
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
				if entityID == nil || *entityID == "" {
					return "", fmt.Errorf("artist ingestion job %d completed without an entity ID", jobID)
				}
				return *entityID, nil
			case "failed":
				if failure != nil {
					return "", fmt.Errorf("artist ingestion failed: %s", *failure)
				}
				return "", fmt.Errorf("artist ingestion failed")
			}
		}
		var riverState rivertype.JobState
		if stateErr := runtime.DB.QueryRow(cmd.Context(), `SELECT state FROM river_job WHERE id=$1`, jobID).Scan(&riverState); stateErr == nil && (riverState == rivertype.JobStateCancelled || riverState == rivertype.JobStateDiscarded) {
			return "", fmt.Errorf("artist ingestion job %d ended in state %s", jobID, riverState)
		}
		select {
		case <-cmd.Context().Done():
			return "", cmd.Context().Err()
		case <-deadline.C:
			return "", fmt.Errorf("timed out after %s waiting for artist ingestion job %d", wait, jobID)
		case <-ticker.C:
		}
	}
}

func outputArtistQueued(jobID int64, provider, providerID string) error {
	if ui.JSONMode {
		return ui.OutputJSON(map[string]any{"job_id": jobID, "state": "queued", "provider": provider, "provider_id": providerID})
	}
	ui.Success("Queued %s artist %s as job %d", provider, providerID, jobID)
	return nil
}

func outputArtistUpdateQueued(jobID int64, entityID string) error {
	if ui.JSONMode {
		return ui.OutputJSON(map[string]any{"job_id": jobID, "state": "queued", "entity_id": entityID})
	}
	ui.Success("Queued artist update as job %d", jobID)
	ui.Info("Entity", entityID)
	return nil
}

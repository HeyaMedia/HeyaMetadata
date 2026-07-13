package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/musiccatalog"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
)

const (
	ArtistCatalogSyncKind      = "artist_catalog_sync_v1"
	ArtistCatalogSchedulerKind = "artist_catalog_scheduler_v1"
)

type ArtistCatalogSyncArgs struct {
	ArtistEntityID string `json:"artist_entity_id" river:"unique"`
	MusicBrainzID  string `json:"musicbrainz_id" river:"unique"`
}

func (ArtistCatalogSyncArgs) Kind() string { return ArtistCatalogSyncKind }
func (ArtistCatalogSyncArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       CatalogQueue,
		Priority:    PriorityCatalog,
		MaxAttempts: 5,
		UniqueOpts:  river.UniqueOpts{ByArgs: true, ByState: activeJobStates()},
	}
}

func InsertArtistCatalog(ctx context.Context, client *river.Client[pgx.Tx], artistEntityID, musicBrainzID string) error {
	_, err := client.Insert(ctx, ArtistCatalogSyncArgs{
		ArtistEntityID: artistEntityID,
		MusicBrainzID:  musicBrainzID,
	}, nil)
	return err
}

type ArtistCatalogSyncWorker struct {
	river.WorkerDefaults[ArtistCatalogSyncArgs]
	runtime *platform.Runtime
}

func NewArtistCatalogSyncWorker(runtime *platform.Runtime) *ArtistCatalogSyncWorker {
	return &ArtistCatalogSyncWorker{runtime: runtime}
}

func (w *ArtistCatalogSyncWorker) Timeout(*river.Job[ArtistCatalogSyncArgs]) time.Duration {
	return 15 * time.Minute
}

func (w *ArtistCatalogSyncWorker) Work(ctx context.Context, job *river.Job[ArtistCatalogSyncArgs]) error {
	result, err := musiccatalog.SyncArtist(ctx, w.runtime, job.Args.ArtistEntityID, job.Args.MusicBrainzID, job.ID)
	if err != nil {
		var status *providers.StatusError
		if errors.As(err, &status) {
			switch status.StatusCode {
			case http.StatusNotFound:
				return river.JobCancel(err)
			case http.StatusTooManyRequests:
				return river.JobSnooze(2 * time.Minute)
			}
		}
		return fmt.Errorf("sync artist catalog: %w", err)
	}
	slog.InfoContext(ctx, "artist catalog reconciled",
		"artist_entity_id", job.Args.ArtistEntityID,
		"musicbrainz_id", job.Args.MusicBrainzID,
		"candidates", result.Candidates,
		"gated_candidates", result.Gated,
		"clusters", result.Clusters,
		"public_clusters", result.PublicClusters,
	)

	client := river.ClientFromContext[pgx.Tx](ctx)
	for _, group := range result.ReleaseGroups {
		if _, err := client.Insert(ctx, ReleaseGroupIngestArgs{
			MusicBrainzID: group.ID,
			Reason:        "artist_catalog",
		}, &river.InsertOpts{Queue: BackgroundQueue, Priority: PriorityScheduled}); err != nil {
			return fmt.Errorf("enqueue release group %s: %w", group.ID, err)
		}
	}
	return nil
}

type ArtistCatalogSchedulerArgs struct{}

func (ArtistCatalogSchedulerArgs) Kind() string { return ArtistCatalogSchedulerKind }
func (ArtistCatalogSchedulerArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       river.QueueDefault,
		Priority:    PriorityScheduled,
		MaxAttempts: 3,
		UniqueOpts:  river.UniqueOpts{ByArgs: true, ByState: activeJobStates()},
	}
}

type ArtistCatalogSchedulerWorker struct {
	river.WorkerDefaults[ArtistCatalogSchedulerArgs]
	runtime *platform.Runtime
}

func NewArtistCatalogSchedulerWorker(runtime *platform.Runtime) *ArtistCatalogSchedulerWorker {
	return &ArtistCatalogSchedulerWorker{runtime: runtime}
}

func (w *ArtistCatalogSchedulerWorker) Work(ctx context.Context, _ *river.Job[ArtistCatalogSchedulerArgs]) error {
	rows, err := w.runtime.DB.Query(ctx, `
		SELECT c.entity_id::text, c.normalized_value
		FROM external_id_claims c
		WHERE c.entity_kind = 'artist'
		  AND c.provider = 'musicbrainz'
		  AND c.namespace = 'artist'
		  AND c.state = 'accepted'
		  AND EXISTS (
			SELECT 1 FROM normalized_records n
			WHERE n.entity_id = c.entity_id
			  AND n.entity_kind = 'artist'
			  AND n.provider = 'musicbrainz'
			  AND n.provider_namespace = 'artist'
			  AND n.provider_record_id = c.normalized_value
		  )
		  AND NOT EXISTS (
			SELECT 1 FROM artist_catalog_sync_runs r
			WHERE r.artist_entity_id = c.entity_id
			  AND r.state = 'completed'
			  AND r.sync_version = $1
			  AND r.completed_at > now() - interval '7 days'
		  )
		ORDER BY c.last_observed_at DESC
		LIMIT 25`, musiccatalog.SyncVersion)
	if err != nil {
		return fmt.Errorf("select artist catalogs: %w", err)
	}
	defer rows.Close()

	type artist struct{ entityID, mbid string }
	artists := make([]artist, 0, 25)
	for rows.Next() {
		var value artist
		if err := rows.Scan(&value.entityID, &value.mbid); err != nil {
			return err
		}
		artists = append(artists, value)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	client := river.ClientFromContext[pgx.Tx](ctx)
	for _, value := range artists {
		if err := InsertArtistCatalog(ctx, client, value.entityID, value.mbid); err != nil {
			return fmt.Errorf("enqueue catalog for artist %s: %w", value.entityID, err)
		}
	}
	return nil
}

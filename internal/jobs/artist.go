package jobs

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/artists"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/HeyaMedia/HeyaMetadata/internal/providercredentials"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

const ArtistIngestKind = "artist_ingest_v1"

type ArtistIngestArgs struct {
	MusicBrainzID string `json:"musicbrainz_id" river:"unique"`
	CredentialRef string `json:"credential_ref,omitempty"`
	Reason        string `json:"reason,omitempty"`
}

func (ArtistIngestArgs) Kind() string { return ArtistIngestKind }
func (ArtistIngestArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: 5, Priority: PriorityInteractive, UniqueOpts: river.UniqueOpts{ByArgs: true, ByState: activeJobStates()}}
}

func InsertArtist(ctx context.Context, runtime *platform.Runtime, client *river.Client[pgx.Tx], args ArtistIngestArgs, priority int) (*rivertype.JobInsertResult, error) {
	inserted, err := client.Insert(ctx, args, &river.InsertOpts{Priority: priority})
	if err != nil {
		return nil, err
	}
	tag, err := runtime.DB.Exec(ctx, `UPDATE river_job SET priority=LEAST(priority,$2),args=CASE WHEN $3='' THEN args ELSE jsonb_set(args,'{credential_ref}',to_jsonb($3::text),true) END WHERE id=$1 AND state IN ('available','pending','retryable','scheduled')`, inserted.Job.ID, priority, args.CredentialRef)
	if err != nil {
		return nil, fmt.Errorf("promote artist ingestion job: %w", err)
	}
	if tag.RowsAffected() == 0 && args.CredentialRef != "" {
		_ = providercredentials.Delete(context.WithoutCancel(ctx), runtime.Redis, args.CredentialRef)
	}
	return inserted, nil
}

type ArtistIngestWorker struct {
	river.WorkerDefaults[ArtistIngestArgs]
	service *artists.Service
	runtime *platform.Runtime
}

func NewArtistIngestWorker(runtime *platform.Runtime) *ArtistIngestWorker {
	return &ArtistIngestWorker{service: artists.NewService(runtime), runtime: runtime}
}
func (w *ArtistIngestWorker) Work(ctx context.Context, job *river.Job[ArtistIngestArgs]) error {
	credentials, err := providercredentials.Load(ctx, w.runtime.Redis, job.Args.CredentialRef)
	if err != nil {
		return river.JobCancel(err)
	}
	result, err := w.service.IngestMusicBrainz(ctx, job.Args.MusicBrainzID, job.ID, credentials)
	if err != nil {
		wrapped := fmt.Errorf("ingest MusicBrainz artist %s: %w", job.Args.MusicBrainzID, err)
		var status *providers.StatusError
		if errors.As(err, &status) {
			switch status.StatusCode {
			case http.StatusNotFound:
				_ = providercredentials.Delete(context.WithoutCancel(ctx), w.runtime.Redis, job.Args.CredentialRef)
				return river.JobCancel(wrapped)
			case http.StatusTooManyRequests:
				return river.JobSnooze(2 * time.Minute)
			}
		}
		return wrapped
	}
	client := river.ClientFromContext[pgx.Tx](ctx)
	if err := InsertArtistCatalog(ctx, client, result.EntityID, job.Args.MusicBrainzID); err != nil {
		return fmt.Errorf("enqueue artist catalog: %w", err)
	}
	_ = providercredentials.Delete(context.WithoutCancel(ctx), w.runtime.Redis, job.Args.CredentialRef)
	return nil
}

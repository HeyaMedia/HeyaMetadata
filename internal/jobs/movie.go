package jobs

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/movies"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/HeyaMedia/HeyaMetadata/internal/providercredentials"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

const MovieIngestKind = "movie_ingest_v1"

const (
	PriorityInteractive = 1
	PriorityStaleRead   = 2
	PriorityCatalog     = 3
	PriorityScheduled   = 4
)

type MovieIngestArgs struct {
	TMDBID        int64  `json:"tmdb_id" river:"unique"`
	CredentialRef string `json:"credential_ref,omitempty"`
	Reason        string `json:"reason,omitempty"`
}

func InsertMovie(ctx context.Context, runtime *platform.Runtime, client *river.Client[pgx.Tx], args MovieIngestArgs, priority int) (*rivertype.JobInsertResult, error) {
	inserted, err := client.Insert(ctx, args, &river.InsertOpts{Priority: priority})
	if err != nil {
		return nil, err
	}
	// A low-priority unique job may already exist when a person asks for the
	// entity. Promote it in place, and attach any transient credential reference
	// while it is still waiting to run.
	commandTag, err := runtime.DB.Exec(ctx, `
		UPDATE river_job
		SET queue = $5,
			priority = LEAST(priority, $2),
			args = CASE WHEN $4 = '' THEN
				CASE WHEN $3 = '' THEN args ELSE jsonb_set(args, '{credential_ref}', to_jsonb($3::text), true) END
			ELSE jsonb_set(
				CASE WHEN $3 = '' THEN args ELSE jsonb_set(args, '{credential_ref}', to_jsonb($3::text), true) END,
				'{reason}', to_jsonb($4::text), true)
			END
		WHERE id = $1 AND state IN ('available', 'pending', 'retryable', 'scheduled')`,
		inserted.Job.ID, priority, args.CredentialRef, args.Reason, MovieQueue)
	if err != nil {
		return nil, fmt.Errorf("promote movie ingestion job: %w", err)
	}
	if commandTag.RowsAffected() == 0 && args.CredentialRef != "" {
		_ = providercredentials.Delete(context.WithoutCancel(ctx), runtime.Redis, args.CredentialRef)
	}
	return inserted, nil
}

func (MovieIngestArgs) Kind() string { return MovieIngestKind }

func (MovieIngestArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       MovieQueue,
		MaxAttempts: 5,
		Priority:    PriorityInteractive,
		UniqueOpts: river.UniqueOpts{
			ByArgs:  true,
			ByState: activeJobStates(),
		},
	}
}

type MovieIngestWorker struct {
	river.WorkerDefaults[MovieIngestArgs]
	service *movies.Service
	runtime *platform.Runtime
}

func NewMovieIngestWorker(runtime *platform.Runtime) *MovieIngestWorker {
	return &MovieIngestWorker{service: movies.NewService(runtime), runtime: runtime}
}

func (w *MovieIngestWorker) Work(ctx context.Context, job *river.Job[MovieIngestArgs]) error {
	credentials, err := providercredentials.Load(ctx, w.runtime.Redis, job.Args.CredentialRef)
	if err != nil {
		return river.JobCancel(err)
	}
	if _, err := w.service.IngestTMDBWithCredentials(ctx, job.Args.TMDBID, job.ID, credentials); err != nil {
		wrapped := fmt.Errorf("ingest TMDB movie %d: %w", job.Args.TMDBID, err)
		var statusError *providers.StatusError
		if errors.As(err, &statusError) {
			switch statusError.StatusCode {
			case http.StatusNotFound:
				_ = providercredentials.Delete(context.WithoutCancel(ctx), w.runtime.Redis, job.Args.CredentialRef)
				return river.JobCancel(wrapped)
			case http.StatusTooManyRequests:
				return river.JobSnooze(5 * time.Minute)
			}
		}
		return wrapped
	}
	_ = providercredentials.Delete(context.WithoutCancel(ctx), w.runtime.Redis, job.Args.CredentialRef)
	return nil
}

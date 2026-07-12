package jobs

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	animeservice "github.com/HeyaMedia/HeyaMetadata/internal/anime"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/HeyaMedia/HeyaMetadata/internal/providercredentials"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers"
	"github.com/HeyaMedia/HeyaMetadata/internal/tvshows"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

const TVShowIngestKind = "tv_show_ingest_v1"
const AnimeIngestKind = "anime_ingest_v1"

type TVShowIngestArgs struct {
	TVMazeID      string `json:"tvmaze_id" river:"unique"`
	CredentialRef string `json:"credential_ref,omitempty"`
	Reason        string `json:"reason,omitempty"`
}

func (TVShowIngestArgs) Kind() string { return TVShowIngestKind }
func (TVShowIngestArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: 5, Priority: PriorityInteractive, UniqueOpts: river.UniqueOpts{ByArgs: true, ByState: activeJobStates()}}
}

type AnimeIngestArgs struct {
	AniDBID       string `json:"anidb_id" river:"unique"`
	CredentialRef string `json:"credential_ref,omitempty"`
	Reason        string `json:"reason,omitempty"`
}

func (AnimeIngestArgs) Kind() string { return AnimeIngestKind }
func (AnimeIngestArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: 5, Priority: PriorityInteractive, UniqueOpts: river.UniqueOpts{ByArgs: true, ByState: activeJobStates()}}
}
func InsertTVShow(ctx context.Context, runtime *platform.Runtime, client *river.Client[pgx.Tx], args TVShowIngestArgs, priority int) (*rivertype.JobInsertResult, error) {
	inserted, err := client.Insert(ctx, args, &river.InsertOpts{Priority: priority})
	if err != nil {
		return nil, err
	}
	tag, err := runtime.DB.Exec(ctx, `UPDATE river_job SET priority=LEAST(priority,$2),args=CASE WHEN $3='' THEN args ELSE jsonb_set(args,'{credential_ref}',to_jsonb($3::text),true) END WHERE id=$1 AND state IN ('available','pending','retryable','scheduled')`, inserted.Job.ID, priority, args.CredentialRef)
	if err == nil && tag.RowsAffected() == 0 && args.CredentialRef != "" {
		_ = providercredentials.Delete(context.WithoutCancel(ctx), runtime.Redis, args.CredentialRef)
	}
	return inserted, err
}
func InsertAnime(ctx context.Context, runtime *platform.Runtime, client *river.Client[pgx.Tx], args AnimeIngestArgs, priority int) (*rivertype.JobInsertResult, error) {
	inserted, err := client.Insert(ctx, args, &river.InsertOpts{Priority: priority})
	if err != nil {
		return nil, err
	}
	tag, err := runtime.DB.Exec(ctx, `UPDATE river_job SET priority=LEAST(priority,$2),args=CASE WHEN $3='' THEN args ELSE jsonb_set(args,'{credential_ref}',to_jsonb($3::text),true) END WHERE id=$1 AND state IN ('available','pending','retryable','scheduled')`, inserted.Job.ID, priority, args.CredentialRef)
	if err == nil && tag.RowsAffected() == 0 && args.CredentialRef != "" {
		_ = providercredentials.Delete(context.WithoutCancel(ctx), runtime.Redis, args.CredentialRef)
	}
	return inserted, err
}

type TVShowIngestWorker struct {
	river.WorkerDefaults[TVShowIngestArgs]
	service *tvshows.Service
	runtime *platform.Runtime
}

func NewTVShowIngestWorker(runtime *platform.Runtime) *TVShowIngestWorker {
	return &TVShowIngestWorker{service: tvshows.NewService(runtime), runtime: runtime}
}
func (w *TVShowIngestWorker) Work(ctx context.Context, job *river.Job[TVShowIngestArgs]) error {
	credentials, err := providercredentials.Load(ctx, w.runtime.Redis, job.Args.CredentialRef)
	if err != nil {
		return river.JobCancel(err)
	}
	_, err = w.service.IngestTVMazeWithCredentials(ctx, job.Args.TVMazeID, job.ID, credentials)
	if err == nil {
		_ = providercredentials.Delete(context.WithoutCancel(ctx), w.runtime.Redis, job.Args.CredentialRef)
	}
	return classifyEpisodicError("TVMaze show "+job.Args.TVMazeID, err)
}

type AnimeIngestWorker struct {
	river.WorkerDefaults[AnimeIngestArgs]
	service *animeservice.Service
	runtime *platform.Runtime
}

func NewAnimeIngestWorker(runtime *platform.Runtime) *AnimeIngestWorker {
	return &AnimeIngestWorker{service: animeservice.NewService(runtime), runtime: runtime}
}
func (w *AnimeIngestWorker) Work(ctx context.Context, job *river.Job[AnimeIngestArgs]) error {
	credentials, err := providercredentials.Load(ctx, w.runtime.Redis, job.Args.CredentialRef)
	if err != nil {
		return river.JobCancel(err)
	}
	_, err = w.service.IngestAniDBWithCredentials(ctx, job.Args.AniDBID, job.ID, credentials)
	if err == nil {
		_ = providercredentials.Delete(context.WithoutCancel(ctx), w.runtime.Redis, job.Args.CredentialRef)
	}
	return classifyEpisodicError("AniDB anime "+job.Args.AniDBID, err)
}
func classifyEpisodicError(label string, err error) error {
	if err == nil {
		return nil
	}
	wrapped := fmt.Errorf("ingest %s: %w", label, err)
	var status *providers.StatusError
	if errors.As(err, &status) {
		if status.StatusCode == http.StatusNotFound {
			return river.JobCancel(wrapped)
		}
		if status.StatusCode == http.StatusTooManyRequests {
			return river.JobSnooze(2 * time.Minute)
		}
	}
	return wrapped
}

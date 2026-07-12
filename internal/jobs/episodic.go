package jobs

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	animeservice "github.com/HeyaMedia/HeyaMetadata/internal/anime"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers"
	"github.com/HeyaMedia/HeyaMetadata/internal/tvshows"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

const TVShowIngestKind = "tv_show_ingest_v1"
const AnimeIngestKind = "anime_ingest_v1"

type TVShowIngestArgs struct {
	TVMazeID string `json:"tvmaze_id" river:"unique"`
	Reason   string `json:"reason,omitempty"`
}

func (TVShowIngestArgs) Kind() string { return TVShowIngestKind }
func (TVShowIngestArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: 5, Priority: PriorityInteractive, UniqueOpts: river.UniqueOpts{ByArgs: true, ByState: activeJobStates()}}
}

type AnimeIngestArgs struct {
	AniDBID string `json:"anidb_id" river:"unique"`
	Reason  string `json:"reason,omitempty"`
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
	_, err = runtime.DB.Exec(ctx, `UPDATE river_job SET priority=LEAST(priority,$2) WHERE id=$1 AND state IN ('available','pending','retryable','scheduled')`, inserted.Job.ID, priority)
	return inserted, err
}
func InsertAnime(ctx context.Context, runtime *platform.Runtime, client *river.Client[pgx.Tx], args AnimeIngestArgs, priority int) (*rivertype.JobInsertResult, error) {
	inserted, err := client.Insert(ctx, args, &river.InsertOpts{Priority: priority})
	if err != nil {
		return nil, err
	}
	_, err = runtime.DB.Exec(ctx, `UPDATE river_job SET priority=LEAST(priority,$2) WHERE id=$1 AND state IN ('available','pending','retryable','scheduled')`, inserted.Job.ID, priority)
	return inserted, err
}

type TVShowIngestWorker struct {
	river.WorkerDefaults[TVShowIngestArgs]
	service *tvshows.Service
}

func NewTVShowIngestWorker(runtime *platform.Runtime) *TVShowIngestWorker {
	return &TVShowIngestWorker{service: tvshows.NewService(runtime)}
}
func (w *TVShowIngestWorker) Work(ctx context.Context, job *river.Job[TVShowIngestArgs]) error {
	_, err := w.service.IngestTVMaze(ctx, job.Args.TVMazeID, job.ID)
	return classifyEpisodicError("TVMaze show "+job.Args.TVMazeID, err)
}

type AnimeIngestWorker struct {
	river.WorkerDefaults[AnimeIngestArgs]
	service *animeservice.Service
}

func NewAnimeIngestWorker(runtime *platform.Runtime) *AnimeIngestWorker {
	return &AnimeIngestWorker{service: animeservice.NewService(runtime)}
}
func (w *AnimeIngestWorker) Work(ctx context.Context, job *river.Job[AnimeIngestArgs]) error {
	_, err := w.service.IngestAniDB(ctx, job.Args.AniDBID, job.ID)
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

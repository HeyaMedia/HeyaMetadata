package jobs

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/movies"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

const MovieIngestKind = "movie_ingest_v1"

type MovieIngestArgs struct {
	TMDBID int64 `json:"tmdb_id" river:"unique"`
}

func (MovieIngestArgs) Kind() string { return MovieIngestKind }

func (MovieIngestArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		MaxAttempts: 5,
		Priority:    1,
		UniqueOpts: river.UniqueOpts{
			ByArgs: true,
			ByState: []rivertype.JobState{
				rivertype.JobStateAvailable,
				rivertype.JobStatePending,
				rivertype.JobStateRunning,
				rivertype.JobStateRetryable,
				rivertype.JobStateScheduled,
			},
		},
	}
}

type MovieIngestWorker struct {
	river.WorkerDefaults[MovieIngestArgs]
	service *movies.Service
}

func NewMovieIngestWorker(runtime *platform.Runtime) *MovieIngestWorker {
	return &MovieIngestWorker{service: movies.NewService(runtime)}
}

func (w *MovieIngestWorker) Work(ctx context.Context, job *river.Job[MovieIngestArgs]) error {
	if _, err := w.service.IngestTMDB(ctx, job.Args.TMDBID, job.ID); err != nil {
		wrapped := fmt.Errorf("ingest TMDB movie %d: %w", job.Args.TMDBID, err)
		var statusError *providers.StatusError
		if errors.As(err, &statusError) {
			switch statusError.StatusCode {
			case http.StatusNotFound:
				return river.JobCancel(wrapped)
			case http.StatusTooManyRequests:
				return river.JobSnooze(5 * time.Minute)
			}
		}
		return wrapped
	}
	return nil
}

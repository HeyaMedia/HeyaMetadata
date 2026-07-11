package jobs

import (
	"fmt"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
)

const CompletedJobRetention = 24 * time.Hour

func Workers(runtime *platform.Runtime) *river.Workers {
	workers := river.NewWorkers()
	river.AddWorker(workers, NewPlatformSmokeWorker(runtime))
	river.AddWorker(workers, NewMovieIngestWorker(runtime))
	river.AddWorker(workers, NewArtistIngestWorker(runtime))
	river.AddWorker(workers, NewBlobRetentionWorker(runtime))
	river.AddWorker(workers, NewRefreshSchedulerWorker(runtime))
	river.AddWorker(workers, NewSourceCollectWorker(runtime))
	return workers
}

func NewClient(runtime *platform.Runtime, maxWorkers int, work bool) (*river.Client[pgx.Tx], error) {
	config := &river.Config{
		Workers:                     Workers(runtime),
		CompletedJobRetentionPeriod: CompletedJobRetention,
		PeriodicJobs: []*river.PeriodicJob{
			river.NewPeriodicJob(
				river.PeriodicInterval(time.Hour),
				func() (river.JobArgs, *river.InsertOpts) { return BlobRetentionArgs{}, nil },
				&river.PeriodicJobOpts{ID: "provider-blob-retention", RunOnStart: true},
			),
			river.NewPeriodicJob(
				river.PeriodicInterval(time.Hour),
				func() (river.JobArgs, *river.InsertOpts) { return RefreshSchedulerArgs{}, nil },
				&river.PeriodicJobOpts{ID: "adaptive-provider-refresh", RunOnStart: true},
			),
		},
	}
	if work {
		config.Queues = map[string]river.QueueConfig{
			river.QueueDefault: {MaxWorkers: maxWorkers},
		}
	}
	client, err := river.NewClient(riverpgxv5.New(runtime.DB), config)
	if err != nil {
		return nil, fmt.Errorf("create River client: %w", err)
	}
	return client, nil
}

package jobs

import (
	"fmt"

	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
)

func Workers(runtime *platform.Runtime) *river.Workers {
	workers := river.NewWorkers()
	river.AddWorker(workers, NewPlatformSmokeWorker(runtime))
	return workers
}

func NewClient(runtime *platform.Runtime, maxWorkers int, work bool) (*river.Client[pgx.Tx], error) {
	config := &river.Config{Workers: Workers(runtime)}
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

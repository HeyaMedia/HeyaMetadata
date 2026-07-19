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
	river.AddWorker(workers, NewArtistCatalogSyncWorker(runtime))
	river.AddWorker(workers, NewArtistCatalogSchedulerWorker(runtime))
	river.AddWorker(workers, NewImageMaterializeWorker(runtime))
	river.AddWorker(workers, NewImageVariantWorker(runtime))
	river.AddWorker(workers, NewImageMaintenanceWorker(runtime))
	river.AddWorker(workers, NewReleaseGroupIngestWorker(runtime))
	river.AddWorker(workers, NewReleaseIngestWorker(runtime))
	river.AddWorker(workers, NewRecordingIngestWorker(runtime))
	river.AddWorker(workers, NewMusicalWorkIngestWorker(runtime))
	river.AddWorker(workers, NewRecordingEvidenceRefreshWorker(runtime))
	river.AddWorker(workers, NewPersonEnrichWorker(runtime))
	river.AddWorker(workers, NewPersonReconciliationSchedulerWorker(runtime))
	river.AddWorker(workers, NewDiscoverySearchWorker(runtime))
	river.AddWorker(workers, NewTVShowIngestWorker(runtime))
	river.AddWorker(workers, NewAnimeIngestWorker(runtime))
	river.AddWorker(workers, NewBookIngestWorker(runtime))
	river.AddWorker(workers, NewMangaIngestWorker(runtime))
	river.AddWorker(workers, NewFingerprintMatchWorker(runtime))
	river.AddWorker(workers, NewBlobRetentionWorker(runtime))
	river.AddWorker(workers, NewRefreshSchedulerWorker(runtime))
	river.AddWorker(workers, NewSourceCollectWorker(runtime))
	river.AddWorker(workers, NewOutboxDrainWorker(runtime))
	return workers
}

func NewClient(runtime *platform.Runtime, maxWorkers int, work bool) (*river.Client[pgx.Tx], error) {
	config := &river.Config{
		Workers:                     Workers(runtime),
		CompletedJobRetentionPeriod: CompletedJobRetention,
		PeriodicJobs: []*river.PeriodicJob{
			river.NewPeriodicJob(
				river.PeriodicInterval(time.Minute),
				func() (river.JobArgs, *river.InsertOpts) { return OutboxDrainArgs{}, nil },
				&river.PeriodicJobOpts{ID: "transactional-outbox-drain", RunOnStart: true},
			),
			river.NewPeriodicJob(
				river.PeriodicInterval(time.Hour),
				func() (river.JobArgs, *river.InsertOpts) { return BlobRetentionArgs{}, nil },
				&river.PeriodicJobOpts{ID: "provider-blob-retention", RunOnStart: true},
			),
			river.NewPeriodicJob(
				river.PeriodicInterval(time.Hour),
				func() (river.JobArgs, *river.InsertOpts) { return ImageMaintenanceArgs{}, nil },
				&river.PeriodicJobOpts{ID: "image-cache-maintenance", RunOnStart: true},
			),
			river.NewPeriodicJob(
				river.PeriodicInterval(time.Hour),
				func() (river.JobArgs, *river.InsertOpts) { return RefreshSchedulerArgs{}, nil },
				&river.PeriodicJobOpts{ID: "adaptive-provider-refresh", RunOnStart: true},
			),
			river.NewPeriodicJob(
				river.PeriodicInterval(time.Hour),
				func() (river.JobArgs, *river.InsertOpts) { return ArtistCatalogSchedulerArgs{}, nil },
				&river.PeriodicJobOpts{ID: "artist-catalog-refresh", RunOnStart: true},
			),
			river.NewPeriodicJob(
				river.PeriodicInterval(10*time.Minute),
				func() (river.JobArgs, *river.InsertOpts) { return PersonReconciliationSchedulerArgs{}, nil },
				&river.PeriodicJobOpts{ID: "person-identity-reconciliation", RunOnStart: true},
			),
		},
	}
	if work {
		config.Queues = queueConfig(maxWorkers, runtime.Config.Worker.ImageMaxWorkers)
	}
	client, err := river.NewClient(riverpgxv5.New(runtime.DB), config)
	if err != nil {
		return nil, fmt.Errorf("create River client: %w", err)
	}
	return client, nil
}

func queueConfig(maxWorkers, imageMaxWorkers int) map[string]river.QueueConfig {
	return map[string]river.QueueConfig{
		river.QueueDefault: {MaxWorkers: maxWorkers},
		MusicQueue:         {MaxWorkers: maxWorkers},
		MovieQueue:         {MaxWorkers: maxWorkers},
		TVQueue:            {MaxWorkers: maxWorkers},
		AnimeQueue:         {MaxWorkers: maxWorkers},
		BooksQueue:         {MaxWorkers: maxWorkers},
		BackgroundQueue:    {MaxWorkers: 1},
		CatalogQueue:       {MaxWorkers: 2},
		ImageQueue:         {MaxWorkers: imageMaxWorkers},
	}
}

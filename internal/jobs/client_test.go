package jobs

import (
	"testing"
	"time"

	"github.com/riverqueue/river"
)

func TestCompletedJobsAreRetainedForOneDay(t *testing.T) {
	t.Parallel()
	if CompletedJobRetention != 24*time.Hour {
		t.Fatalf("completed job retention: %s", CompletedJobRetention)
	}
}

func TestPriorityBandsReserveCapacityForInteractiveWork(t *testing.T) {
	t.Parallel()
	if !(PriorityInteractive < PriorityStaleRead && PriorityStaleRead < PriorityCatalog && PriorityCatalog < PriorityScheduled) {
		t.Fatalf("priority order: interactive=%d stale=%d catalog=%d scheduled=%d", PriorityInteractive, PriorityStaleRead, PriorityCatalog, PriorityScheduled)
	}
	if adaptiveArtistPriority != PriorityCatalog || adaptiveArtistPriority >= PriorityScheduled {
		t.Fatalf("adaptive artist priority must precede scheduled child work: artist=%d child=%d", adaptiveArtistPriority, PriorityScheduled)
	}
	if got := (MovieIngestArgs{TMDBID: 603}).InsertOpts().Priority; got != PriorityInteractive {
		t.Fatalf("default movie priority: %d", got)
	}
	if got := (RefreshSchedulerArgs{}).InsertOpts().Priority; got != PriorityScheduled {
		t.Fatalf("scheduler priority: %d", got)
	}
	if opts := (RecordingEvidenceRefreshArgs{RecordingID: "id"}).InsertOpts(); opts.Queue != MusicQueue || opts.Priority != PriorityScheduled {
		t.Fatalf("recording evidence queue/priority: %s/%d", opts.Queue, opts.Priority)
	}
	if opts := (ArtistCatalogSyncArgs{ArtistEntityID: "entity", MusicBrainzID: "mbid"}).InsertOpts(); opts.Queue != MusicQueue || opts.Priority != PriorityCatalog {
		t.Fatalf("artist catalog queue/priority: %s/%d", opts.Queue, opts.Priority)
	}
	if opts := (ArtistCatalogSchedulerArgs{}).InsertOpts(); opts.Queue != river.QueueDefault || opts.Priority != PriorityScheduled {
		t.Fatalf("artist catalog scheduler queue/priority: %s/%d", opts.Queue, opts.Priority)
	}
	if opts := (PersonReconciliationSchedulerArgs{}).InsertOpts(); opts.Queue != BackgroundQueue || opts.Priority != PriorityScheduled {
		t.Fatalf("person reconciliation scheduler queue/priority: %s/%d", opts.Queue, opts.Priority)
	}
	if opts := (ImageMaterializeArgs{ImageID: "id"}).InsertOpts(); opts.Queue != ImageQueue || opts.Priority != PriorityInteractive {
		t.Fatalf("image queue/priority: %s/%d", opts.Queue, opts.Priority)
	}
	if opts := (ImageVariantArgs{ImageID: "id", Width: 640}).InsertOpts(); opts.Queue != ImageQueue || opts.Priority != PriorityInteractive {
		t.Fatalf("image variant queue/priority: %s/%d", opts.Queue, opts.Priority)
	}
}

func TestDomainAndImageQueuesHaveIndependentConcurrency(t *testing.T) {
	t.Parallel()
	queues := queueConfig(8, 12)
	if got := queues[river.QueueDefault].MaxWorkers; got != 8 {
		t.Fatalf("default queue workers: %d", got)
	}
	if got := queues[ImageQueue].MaxWorkers; got != 12 {
		t.Fatalf("image queue workers: %d", got)
	}
	for _, queue := range []string{MusicQueue, MovieQueue, TVQueue, AnimeQueue, BooksQueue} {
		if got := queues[queue].MaxWorkers; got != 8 {
			t.Errorf("%s queue workers: %d", queue, got)
		}
	}
}

func TestMetadataJobsRouteToDomainQueues(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		queue string
		want  string
	}{
		{"movie", (MovieIngestArgs{}).InsertOpts().Queue, MovieQueue},
		{"artist", (ArtistIngestArgs{}).InsertOpts().Queue, MusicQueue},
		{"artist catalog", (ArtistCatalogSyncArgs{}).InsertOpts().Queue, MusicQueue},
		{"release group", (ReleaseGroupIngestArgs{}).InsertOpts().Queue, MusicQueue},
		{"release", (ReleaseIngestArgs{}).InsertOpts().Queue, MusicQueue},
		{"recording", (RecordingIngestArgs{}).InsertOpts().Queue, MusicQueue},
		{"recording evidence", (RecordingEvidenceRefreshArgs{}).InsertOpts().Queue, MusicQueue},
		{"musical work", (MusicalWorkIngestArgs{}).InsertOpts().Queue, MusicQueue},
		{"fingerprint", (FingerprintMatchArgs{}).InsertOpts().Queue, MusicQueue},
		{"tv", (TVShowIngestArgs{}).InsertOpts().Queue, TVQueue},
		{"anime", (AnimeIngestArgs{}).InsertOpts().Queue, AnimeQueue},
		{"book", (BookIngestArgs{}).InsertOpts().Queue, BooksQueue},
		{"manga", (MangaIngestArgs{}).InsertOpts().Queue, BooksQueue},
		{"movie discovery", (DiscoverySearchArgs{MediaKind: "movie"}).InsertOpts().Queue, MovieQueue},
		{"music discovery", (DiscoverySearchArgs{MediaKind: "artist"}).InsertOpts().Queue, MusicQueue},
		{"tv discovery", (DiscoverySearchArgs{MediaKind: "tv_show"}).InsertOpts().Queue, TVQueue},
		{"anime discovery", (DiscoverySearchArgs{MediaKind: "anime"}).InsertOpts().Queue, AnimeQueue},
		{"book discovery", (DiscoverySearchArgs{MediaKind: "book_work"}).InsertOpts().Queue, BooksQueue},
	}
	for _, test := range tests {
		if test.queue != test.want {
			t.Errorf("%s queue: got %q, want %q", test.name, test.queue, test.want)
		}
	}
}

func TestArtistCatalogAllowsPagedDetailEvidence(t *testing.T) {
	t.Parallel()
	worker := &ArtistCatalogSyncWorker{}
	if got := worker.Timeout(nil); got != 15*time.Minute {
		t.Fatalf("artist catalog timeout: %s", got)
	}
}

func TestPersonReconciliationAllowsFullBatch(t *testing.T) {
	t.Parallel()
	worker := &PersonReconciliationSchedulerWorker{}
	if got := worker.Timeout(nil); got != 15*time.Minute {
		t.Fatalf("person reconciliation timeout: %s", got)
	}
}

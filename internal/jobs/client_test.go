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
	if got := (MovieIngestArgs{TMDBID: 603}).InsertOpts().Priority; got != PriorityInteractive {
		t.Fatalf("default movie priority: %d", got)
	}
	if got := (RefreshSchedulerArgs{}).InsertOpts().Priority; got != PriorityScheduled {
		t.Fatalf("scheduler priority: %d", got)
	}
	if opts := (RecordingEvidenceRefreshArgs{RecordingID: "id"}).InsertOpts(); opts.Queue != BackgroundQueue || opts.Priority != PriorityScheduled {
		t.Fatalf("recording evidence queue/priority: %s/%d", opts.Queue, opts.Priority)
	}
	if opts := (ArtistCatalogSyncArgs{ArtistEntityID: "entity", MusicBrainzID: "mbid"}).InsertOpts(); opts.Queue != CatalogQueue || opts.Priority != PriorityCatalog {
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
}

func TestArtistCatalogAllowsPagedDetailEvidence(t *testing.T) {
	t.Parallel()
	worker := &ArtistCatalogSyncWorker{}
	if got := worker.Timeout(nil); got != 15*time.Minute {
		t.Fatalf("artist catalog timeout: %s", got)
	}
}

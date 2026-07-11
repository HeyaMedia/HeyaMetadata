package jobs

import (
	"testing"
	"time"
)

func TestCompletedJobsAreRetainedForOneDay(t *testing.T) {
	t.Parallel()
	if CompletedJobRetention != 24*time.Hour {
		t.Fatalf("completed job retention: %s", CompletedJobRetention)
	}
}

func TestPriorityBandsReserveCapacityForInteractiveWork(t *testing.T) {
	t.Parallel()
	if !(PriorityInteractive < PriorityStaleRead && PriorityStaleRead < PriorityScheduled) {
		t.Fatalf("priority order: interactive=%d stale=%d scheduled=%d", PriorityInteractive, PriorityStaleRead, PriorityScheduled)
	}
	if got := (MovieIngestArgs{TMDBID: 603}).InsertOpts().Priority; got != PriorityInteractive {
		t.Fatalf("default movie priority: %d", got)
	}
	if got := (RefreshSchedulerArgs{}).InsertOpts().Priority; got != PriorityScheduled {
		t.Fatalf("scheduler priority: %d", got)
	}
}

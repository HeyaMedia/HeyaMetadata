package accessstats

import (
	"testing"
	"time"
)

func TestCadenceCoolsFromTwoToThirtyDays(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 11, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		last *time.Time
		want time.Duration
	}{
		{"hot", timePointer(now.Add(-time.Hour)), 2 * 24 * time.Hour},
		{"warm", timePointer(now.Add(-7 * 24 * time.Hour)), 7 * 24 * time.Hour},
		{"cool", timePointer(now.Add(-30 * 24 * time.Hour)), 14 * 24 * time.Hour},
		{"cold", timePointer(now.Add(-90 * 24 * time.Hour)), 30 * 24 * time.Hour},
		{"never", nil, 30 * 24 * time.Hour},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := Cadence(now, test.last, 0, now); got != test.want {
				t.Fatalf("cadence: got %s, want %s", got, test.want)
			}
		})
	}
}

func TestCadenceKeepsFrequentlyFetchedEntityWarm(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	if got := Cadence(now, nil, 25, now); got != 2*24*time.Hour {
		t.Fatalf("high-score cadence: %s", got)
	}
}

func timePointer(value time.Time) *time.Time { return &value }

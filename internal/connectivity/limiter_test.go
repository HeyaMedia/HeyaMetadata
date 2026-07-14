package connectivity

import (
	"context"
	"testing"
	"time"
)

func TestMemoryLimiterEnforcesRateAndInflightLimits(t *testing.T) {
	t.Parallel()
	limiter := NewLimiter(nil)
	now := time.Date(2026, time.July, 14, 12, 0, 0, 0, time.UTC)
	limiter.now = func() time.Time { return now }

	for index := 0; index < 2; index++ {
		allowed, _, err := limiter.Allow(context.Background(), "check", "1.1.1.1", 2, time.Minute)
		if err != nil || !allowed {
			t.Fatalf("request %d unexpectedly rejected: allowed=%v err=%v", index+1, allowed, err)
		}
	}
	allowed, retry, err := limiter.Allow(context.Background(), "check", "1.1.1.1", 2, time.Minute)
	if err != nil || allowed || retry != 60 {
		t.Fatalf("third request: allowed=%v retry=%d err=%v", allowed, retry, err)
	}
	now = now.Add(time.Minute)
	allowed, _, err = limiter.Allow(context.Background(), "check", "1.1.1.1", 2, time.Minute)
	if err != nil || !allowed {
		t.Fatalf("new window was not allowed: allowed=%v err=%v", allowed, err)
	}

	release, acquired, _, err := limiter.Acquire(context.Background(), "1.1.1.1", 15*time.Second)
	if err != nil || !acquired {
		t.Fatalf("first lock: acquired=%v err=%v", acquired, err)
	}
	_, acquired, retry, err = limiter.Acquire(context.Background(), "1.1.1.1", 15*time.Second)
	if err != nil || acquired || retry != 15 {
		t.Fatalf("second lock: acquired=%v retry=%d err=%v", acquired, retry, err)
	}
	release(context.Background())
	_, acquired, _, err = limiter.Acquire(context.Background(), "1.1.1.1", 15*time.Second)
	if err != nil || !acquired {
		t.Fatalf("lock after release: acquired=%v err=%v", acquired, err)
	}
}

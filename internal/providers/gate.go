package providers

import (
	"context"
	"sync"
	"time"
)

// RequestGate spaces actual upstream requests. SharedRequestGate returns the
// same in-process gate for every client using a provider/base-URL pair, so
// concurrent jobs cannot accidentally multiply a provider's configured rate.
// It belongs in HTTPClient's prepare callback so cache hits never wait.
type RequestGate struct {
	mu            sync.Mutex
	interval      time.Duration
	next          time.Time
	deferredUntil time.Time
}

var sharedRequestGates = struct {
	sync.Mutex
	values map[string]*RequestGate
}{values: map[string]*RequestGate{}}

func SharedRequestGate(key string, requestsPerSecond float64) *RequestGate {
	if requestsPerSecond <= 0 {
		requestsPerSecond = 1
	}
	interval := time.Duration(float64(time.Second) / requestsPerSecond)
	if interval < 0 {
		interval = 0
	}
	sharedRequestGates.Lock()
	defer sharedRequestGates.Unlock()
	if gate := sharedRequestGates.values[key]; gate != nil {
		gate.mu.Lock()
		if interval > gate.interval {
			gate.interval = interval
		}
		gate.mu.Unlock()
		return gate
	}
	gate := &RequestGate{interval: interval}
	sharedRequestGates.values[key] = gate
	return gate
}

func (g *RequestGate) Wait(ctx context.Context) error {
	for {
		g.mu.Lock()
		wait := time.Until(g.next)
		if wait <= 0 {
			g.next = time.Now().Add(g.interval)
			g.mu.Unlock()
			return nil
		}
		g.mu.Unlock()

		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		// Re-check under the lock: a provider response may have extended
		// the gate while this caller was waiting for ordinary spacing.
	}
}

// Defer pauses every caller sharing this gate. Providers use it when an
// upstream response reports an exhausted global budget or Retry-After delay.
func (g *RequestGate) Defer(delay time.Duration) {
	if delay <= 0 {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	next := time.Now().Add(delay)
	if next.After(g.next) {
		g.next = next
	}
	if next.After(g.deferredUntil) {
		g.deferredUntil = next
	}
}

// DeferredFor reports an explicit provider-requested cooldown without
// including the gate's ordinary request spacing. Callers can use it to snooze
// durable work instead of occupying a worker while a long cooldown elapses.
func (g *RequestGate) DeferredFor() time.Duration {
	g.mu.Lock()
	defer g.mu.Unlock()
	remaining := time.Until(g.deferredUntil)
	if remaining <= 0 {
		return 0
	}
	return remaining
}

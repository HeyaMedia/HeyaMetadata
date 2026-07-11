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
	mu       sync.Mutex
	interval time.Duration
	next     time.Time
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
	g.mu.Lock()
	defer g.mu.Unlock()
	wait := time.Until(g.next)
	if wait > 0 {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
	}
	g.next = time.Now().Add(g.interval)
	return nil
}

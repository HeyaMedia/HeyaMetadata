package providers

import (
	"context"
	"testing"
	"time"
)

func TestRequestGateSpacesRequestsAndHonorsCancellation(t *testing.T) {
	t.Parallel()
	gate := &RequestGate{interval: 30 * time.Millisecond}
	if err := gate.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	if err := gate.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if time.Since(started) < 20*time.Millisecond {
		t.Fatal("gate did not space requests")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := gate.Wait(ctx); err == nil {
		t.Fatal("cancelled wait unexpectedly succeeded")
	}
}

func TestRequestGateDefersAllCallers(t *testing.T) {
	t.Parallel()
	gate := &RequestGate{}
	gate.Defer(30 * time.Millisecond)
	started := time.Now()
	if err := gate.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if time.Since(started) < 20*time.Millisecond {
		t.Fatal("gate did not honor provider cooldown")
	}
}

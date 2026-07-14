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
	if remaining := gate.DeferredFor(); remaining <= 0 || remaining > 30*time.Millisecond {
		t.Fatalf("deferred for: %s", remaining)
	}
	started := time.Now()
	if err := gate.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if time.Since(started) < 20*time.Millisecond {
		t.Fatal("gate did not honor provider cooldown")
	}
}

func TestRequestGateDeferExtendsAlreadyWaitingCaller(t *testing.T) {
	t.Parallel()
	gate := &RequestGate{interval: 20 * time.Millisecond}
	if err := gate.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	completed := make(chan error, 1)
	started := time.Now()
	go func() { completed <- gate.Wait(context.Background()) }()
	time.Sleep(5 * time.Millisecond)
	gate.Defer(40 * time.Millisecond)
	select {
	case err := <-completed:
		t.Fatalf("wait completed before extended cooldown: %v after %s", err, time.Since(started))
	case <-time.After(30 * time.Millisecond):
	}
	select {
	case err := <-completed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(50 * time.Millisecond):
		t.Fatal("wait did not complete after extended cooldown")
	}
}

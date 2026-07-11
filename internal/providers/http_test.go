package providers

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

type resolverFunc func(context.Context, Payload, func() (Payload, error)) (Payload, error)

func (function resolverFunc) Resolve(ctx context.Context, payload Payload, fetch func() (Payload, error)) (Payload, error) {
	return function(ctx, payload, fetch)
}

func TestHTTPGuardRunsOnlyForNetworkFetch(t *testing.T) {
	t.Parallel()
	request, _ := http.NewRequest(http.MethodGet, "https://example.invalid", nil)
	guardErr := errors.New("credentials missing")

	warm := NewCachedHTTPClient(time.Second, resolverFunc(func(_ context.Context, payload Payload, _ func() (Payload, error)) (Payload, error) {
		payload.FromCache = true
		return payload, nil
	}))
	if payload, err := warm.DoGuarded(context.Background(), request, Payload{Provider: "test"}, func() error { return guardErr }); err != nil || !payload.FromCache {
		t.Fatalf("warm response required credentials: %+v, %v", payload, err)
	}

	cold := NewCachedHTTPClient(time.Second, resolverFunc(func(_ context.Context, _ Payload, fetch func() (Payload, error)) (Payload, error) {
		return fetch()
	}))
	if _, err := cold.DoGuarded(context.Background(), request, Payload{Provider: "test"}, func() error { return guardErr }); !errors.Is(err, guardErr) {
		t.Fatalf("network fetch did not enforce guard: %v", err)
	}
}

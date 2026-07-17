package cdn

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

type fakeStore struct{ stored map[string]string }

func (store *fakeStore) Get(_ context.Context, key string) *redis.StringCmd {
	if value, ok := store.stored[key]; ok {
		return redis.NewStringResult(value, nil)
	}
	return redis.NewStringResult("", redis.Nil)
}

func (store *fakeStore) Set(_ context.Context, key string, value interface{}, _ time.Duration) *redis.StatusCmd {
	store.stored[key] = fmt.Sprint(value)
	return redis.NewStatusResult("OK", nil)
}

func TestPurgeOnDeployPurgesOncePerVersion(t *testing.T) {
	purges := 0
	api := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/zones/zone-1/purge_cache" || request.Header.Get("Authorization") != "Bearer token-1" {
			t.Errorf("unexpected request %s auth=%q", request.URL.Path, request.Header.Get("Authorization"))
		}
		purges++
		_, _ = writer.Write([]byte(`{"success":true}`))
	}))
	defer api.Close()

	store := &fakeStore{stored: map[string]string{}}
	purger := Purger{ZoneID: "zone-1", Token: "token-1", APIBaseURL: api.URL, Store: store}

	for range 2 {
		if err := purger.PurgeOnDeploy(context.Background(), "v1.2.3"); err != nil {
			t.Fatal(err)
		}
	}
	if purges != 1 {
		t.Fatalf("expected exactly one purge, got %d", purges)
	}

	if err := purger.PurgeOnDeploy(context.Background(), "v1.2.4"); err != nil {
		t.Fatal(err)
	}
	if purges != 2 {
		t.Fatalf("expected a second purge for the new version, got %d", purges)
	}
}

func TestPurgeOnDeploySkipsDevAndUnconfigured(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("no request expected")
	}))
	defer api.Close()

	store := &fakeStore{stored: map[string]string{}}
	configured := Purger{ZoneID: "zone-1", Token: "token-1", APIBaseURL: api.URL, Store: store}
	if err := configured.PurgeOnDeploy(context.Background(), "dev"); err != nil {
		t.Fatal(err)
	}
	unconfigured := Purger{APIBaseURL: api.URL, Store: store}
	if err := unconfigured.PurgeOnDeploy(context.Background(), "v1.2.3"); err != nil {
		t.Fatal(err)
	}
}

func TestPurgeOnDeploySurfacesRejection(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusForbidden)
		_, _ = writer.Write([]byte(`{"success":false,"errors":[{"message":"invalid token"}]}`))
	}))
	defer api.Close()

	store := &fakeStore{stored: map[string]string{}}
	purger := Purger{ZoneID: "zone-1", Token: "token-1", APIBaseURL: api.URL, Store: store}
	if err := purger.PurgeOnDeploy(context.Background(), "v1.2.3"); err == nil {
		t.Fatal("expected rejection error")
	}
	if len(store.stored) != 0 {
		t.Fatal("failed purge must not record the version")
	}
}

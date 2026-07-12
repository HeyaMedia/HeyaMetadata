package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/config"
	"github.com/HeyaMedia/HeyaMetadata/internal/discovery"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/HeyaMedia/HeyaMetadata/internal/providercredentials"
)

func TestIntegrationInteractiveRequestPromotesScheduledMovie(t *testing.T) {
	if os.Getenv("HEYA_METADATA_INTEGRATION") != "1" {
		t.Skip("set HEYA_METADATA_INTEGRATION=1 to use the local Postgres and Redis stack")
	}
	ctx := context.Background()
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := platform.Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(runtime.Close)
	client, err := NewClient(runtime, 1, false)
	if err != nil {
		t.Fatal(err)
	}

	const tmdbID = int64(8_765_432_101)
	scheduled, err := InsertMovie(ctx, runtime, client, MovieIngestArgs{TMDBID: tmdbID, Reason: "adaptive_refresh"}, PriorityScheduled)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = runtime.DB.Exec(context.Background(), `DELETE FROM river_job WHERE id = $1`, scheduled.Job.ID)
	})
	reference, err := providercredentials.Store(ctx, runtime.Redis, providercredentials.Credentials{APIKeys: map[string]string{"tmdb": "integration-secret"}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = providercredentials.Delete(context.Background(), runtime.Redis, reference) })

	interactive, err := InsertMovie(ctx, runtime, client, MovieIngestArgs{TMDBID: tmdbID, CredentialRef: reference, Reason: "interactive_resolution"}, PriorityInteractive)
	if err != nil {
		t.Fatal(err)
	}
	if interactive.Job.ID != scheduled.Job.ID {
		t.Fatalf("unique scheduled job was duplicated: %d != %d", interactive.Job.ID, scheduled.Job.ID)
	}
	var priority int
	var argsJSON []byte
	if err := runtime.DB.QueryRow(ctx, `SELECT priority, args FROM river_job WHERE id = $1`, scheduled.Job.ID).Scan(&priority, &argsJSON); err != nil {
		t.Fatal(err)
	}
	var args MovieIngestArgs
	if err := json.Unmarshal(argsJSON, &args); err != nil {
		t.Fatal(err)
	}
	if priority != PriorityInteractive || args.CredentialRef != reference || args.Reason != "interactive_resolution" {
		t.Fatalf("job was not promoted with request context: priority=%d args=%+v", priority, args)
	}
	if strings.Contains(string(argsJSON), "integration-secret") {
		t.Fatal("plaintext provider key entered River args")
	}
}

func TestIntegrationIdenticalDiscoveryCollapsesToOneJob(t *testing.T) {
	if os.Getenv("HEYA_METADATA_INTEGRATION") != "1" {
		t.Skip("set HEYA_METADATA_INTEGRATION=1 to use the local Postgres and Redis stack")
	}
	ctx := context.Background()
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := platform.Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(runtime.Close)
	client, err := NewClient(runtime, 1, false)
	if err != nil {
		t.Fatal(err)
	}
	request := discovery.Request{Kind: discovery.KindArtist, Query: fmt.Sprintf("integration-discovery-%d", time.Now().UnixNano()), Hints: discovery.Hints{Country: "JP"}}
	run, err := discovery.EnsureRun(ctx, runtime, request)
	if err != nil {
		t.Fatal(err)
	}
	firstRef, err := providercredentials.Store(ctx, runtime.Redis, providercredentials.Credentials{APIKeys: map[string]string{"tmdb": "first-discovery-key"}})
	if err != nil {
		t.Fatal(err)
	}
	secondRef, err := providercredentials.Store(ctx, runtime.Redis, providercredentials.Credentials{APIKeys: map[string]string{"tmdb": "second-discovery-key"}})
	if err != nil {
		t.Fatal(err)
	}
	first, err := InsertDiscovery(ctx, runtime, client, run, firstRef)
	if err != nil {
		t.Fatal(err)
	}
	second, err := InsertDiscovery(ctx, runtime, client, run, secondRef)
	if err != nil {
		t.Fatal(err)
	}
	if first.Job.ID != second.Job.ID {
		t.Fatalf("discovery jobs duplicated: %d != %d", first.Job.ID, second.Job.ID)
	}
	var storedRef string
	if err := runtime.DB.QueryRow(ctx, `SELECT args->>'credential_ref' FROM river_job WHERE id=$1`, first.Job.ID).Scan(&storedRef); err != nil {
		t.Fatal(err)
	}
	if storedRef != secondRef {
		t.Fatalf("new request credential did not replace queued job credential")
	}
	t.Cleanup(func() {
		_ = providercredentials.Delete(context.Background(), runtime.Redis, firstRef)
		_ = providercredentials.Delete(context.Background(), runtime.Redis, secondRef)
		_, _ = runtime.DB.Exec(context.Background(), `DELETE FROM discovery_runs WHERE request_hash=$1`, run.RequestHash)
		_, _ = runtime.DB.Exec(context.Background(), `DELETE FROM river_job WHERE id=$1`, first.Job.ID)
	})
}

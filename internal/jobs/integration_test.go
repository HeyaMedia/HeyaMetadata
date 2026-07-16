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
	"github.com/riverqueue/river"
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
	// Keep the low-priority job pending so a concurrently running local worker
	// cannot claim it before this test exercises the in-place promotion path.
	scheduled, err := client.Insert(ctx, MovieIngestArgs{TMDBID: tmdbID, Reason: "adaptive_refresh"}, &river.InsertOpts{Pending: true, Priority: PriorityScheduled})
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
	var queue string
	var argsJSON []byte
	if err := runtime.DB.QueryRow(ctx, `SELECT queue,priority,args FROM river_job WHERE id = $1`, scheduled.Job.ID).Scan(&queue, &priority, &argsJSON); err != nil {
		t.Fatal(err)
	}
	var args MovieIngestArgs
	if err := json.Unmarshal(argsJSON, &args); err != nil {
		t.Fatal(err)
	}
	if queue != MovieQueue || priority != PriorityInteractive || args.CredentialRef != reference || args.Reason != "interactive_resolution" {
		t.Fatalf("job was not promoted with request context: queue=%s priority=%d args=%+v", queue, priority, args)
	}
	if strings.Contains(string(argsJSON), "integration-secret") {
		t.Fatal("plaintext provider key entered River args")
	}
}

func TestIntegrationOperatorJobJumpsInteractiveBacklog(t *testing.T) {
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

	queue := fmt.Sprintf("operator-integration-%d", time.Now().UnixNano())
	older, err := client.Insert(ctx, MovieIngestArgs{TMDBID: 8_765_432_102}, &river.InsertOpts{Queue: queue, Priority: PriorityInteractive, ScheduledAt: time.Now().Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	operator, err := client.Insert(ctx, MovieIngestArgs{TMDBID: 8_765_432_103}, &river.InsertOpts{Queue: queue, Priority: PriorityInteractive})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = runtime.DB.Exec(context.Background(), `DELETE FROM river_job WHERE id=ANY($1::bigint[])`, []int64{older.Job.ID, operator.Job.ID})
	})

	if err := PromoteOperatorJob(ctx, runtime, operator.Job.ID); err != nil {
		t.Fatal(err)
	}
	var firstID int64
	if err := runtime.DB.QueryRow(ctx, `SELECT id FROM river_job WHERE queue=$1 AND state='available' ORDER BY priority,scheduled_at,id LIMIT 1`, queue).Scan(&firstID); err != nil {
		t.Fatal(err)
	}
	if firstID != operator.Job.ID {
		t.Fatalf("first eligible job=%d want operator job %d", firstID, operator.Job.ID)
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
	var storedRef, mediaKind, queue string
	if err := runtime.DB.QueryRow(ctx, `SELECT queue,args->>'credential_ref',args->>'media_kind' FROM river_job WHERE id=$1`, first.Job.ID).Scan(&queue, &storedRef, &mediaKind); err != nil {
		t.Fatal(err)
	}
	if storedRef != secondRef {
		t.Fatalf("new request credential did not replace queued job credential")
	}
	if queue != MusicQueue || mediaKind != discovery.KindArtist {
		t.Fatalf("discovery routing: queue=%s media_kind=%s", queue, mediaKind)
	}
	t.Cleanup(func() {
		_ = providercredentials.Delete(context.Background(), runtime.Redis, firstRef)
		_ = providercredentials.Delete(context.Background(), runtime.Redis, secondRef)
		_, _ = runtime.DB.Exec(context.Background(), `DELETE FROM discovery_runs WHERE request_hash=$1`, run.RequestHash)
		_, _ = runtime.DB.Exec(context.Background(), `DELETE FROM river_job WHERE id=$1`, first.Job.ID)
	})
}

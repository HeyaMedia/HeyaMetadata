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
	"github.com/google/uuid"
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

func TestIntegrationOutboxDrainerLoopsPastOneBatch(t *testing.T) {
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
	slug := fmt.Sprintf("outbox-drain-integration-%d", time.Now().UnixNano())
	var entityID string
	if err := runtime.DB.QueryRow(ctx, `INSERT INTO entities(kind,slug)VALUES('artist',$1)RETURNING id`, slug).Scan(&entityID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = runtime.DB.Exec(context.Background(), `DELETE FROM change_log WHERE entity_id=$1`, entityID)
		_, _ = runtime.DB.Exec(context.Background(), `DELETE FROM change_outbox WHERE entity_id=$1`, entityID)
		_, _ = runtime.DB.Exec(context.Background(), `DELETE FROM entities WHERE id=$1`, entityID)
	})
	if _, err := runtime.DB.Exec(ctx, `INSERT INTO change_outbox(entity_id,entity_kind,slug,change_type,changed_scopes,projection_version)SELECT $1,'artist',$2,'updated',ARRAY['detail'],value FROM generate_series(1,101) value`, entityID, slug); err != nil {
		t.Fatal(err)
	}
	if err := NewOutboxDrainWorker(runtime).Work(ctx, nil); err != nil {
		t.Fatal(err)
	}
	var pending, sequenced int
	if err := runtime.DB.QueryRow(ctx, `SELECT count(*) FILTER(WHERE outbox.sequenced_at IS NULL),count(log.sequence) FROM change_outbox outbox LEFT JOIN change_log log ON log.outbox_id=outbox.id WHERE outbox.entity_id=$1`, entityID).Scan(&pending, &sequenced); err != nil {
		t.Fatal(err)
	}
	if pending != 0 || sequenced != 101 {
		t.Fatalf("outbox pending=%d sequenced=%d", pending, sequenced)
	}
}

func TestIntegrationArtistMaterializationEnqueuesLegacyRecordingCredit(t *testing.T) {
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
	artistMBID, recordingMBID := uuid.NewString(), uuid.NewString()
	var artistEntityID, recordingEntityID string
	if err := runtime.DB.QueryRow(ctx, `INSERT INTO entities(kind,slug)VALUES('artist',$1)RETURNING id`, "legacy-credit-artist-"+artistMBID).Scan(&artistEntityID); err != nil {
		t.Fatal(err)
	}
	if err := runtime.DB.QueryRow(ctx, `INSERT INTO entities(kind,slug)VALUES('recording',$1)RETURNING id`, "legacy-credit-recording-"+recordingMBID).Scan(&recordingEntityID); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.DB.Exec(ctx, `INSERT INTO external_id_claims(entity_id,entity_kind,provider,namespace,normalized_value,state,confidence,first_observed_at,last_observed_at)VALUES($1,'recording','musicbrainz','recording',$2,'accepted',1,now(),now())`, recordingEntityID, recordingMBID); err != nil {
		t.Fatal(err)
	}
	document := fmt.Sprintf(`{"data":{"artist_credits":[{"artist_provider":"musicbrainz","artist_namespace":"artist","artist_id":%q}]}}`, artistMBID)
	if _, err := runtime.DB.Exec(ctx, `INSERT INTO canonical_recordings(entity_id,merge_version,source_fingerprint,document)VALUES($1,'integration','integration',$2::jsonb)`, recordingEntityID, document); err != nil {
		t.Fatal(err)
	}
	materializedDocument := fmt.Sprintf(`{"data":{"artist_credits":[{"artist_provider":"musicbrainz","artist_namespace":"artist","artist_id":%q,"artist_entity_id":%q,"resolution_state":"materialized"}]}}`, artistMBID, artistEntityID)
	if _, err := runtime.DB.Exec(ctx, `INSERT INTO api_documents(entity_id,document_kind,schema_version,projection_version,document,fresh_until)VALUES($1,'detail',1,1,$2::jsonb,now()+interval '1 day')`, recordingEntityID, materializedDocument); err != nil {
		t.Fatal(err)
	}
	var jobID, artistJobID int64
	t.Cleanup(func() {
		if jobID > 0 {
			_, _ = runtime.DB.Exec(context.Background(), `DELETE FROM river_job WHERE id=$1`, jobID)
		}
		if artistJobID > 0 {
			_, _ = runtime.DB.Exec(context.Background(), `DELETE FROM river_job WHERE id=$1`, artistJobID)
		}
		_, _ = runtime.DB.Exec(context.Background(), `DELETE FROM api_documents WHERE entity_id=$1`, recordingEntityID)
		_, _ = runtime.DB.Exec(context.Background(), `DELETE FROM canonical_recordings WHERE entity_id=$1`, recordingEntityID)
		_, _ = runtime.DB.Exec(context.Background(), `DELETE FROM external_id_claims WHERE entity_id=$1`, recordingEntityID)
		_, _ = runtime.DB.Exec(context.Background(), `DELETE FROM entities WHERE id=ANY($1::uuid[])`, []string{recordingEntityID, artistEntityID})
	})
	enqueueRecordingsAwaitingArtist(ctx, runtime, client, "musicbrainz", artistMBID, artistEntityID)
	if err := runtime.DB.QueryRow(ctx, `SELECT id FROM river_job WHERE kind=$1 AND args->>'musicbrainz_id'=$2 ORDER BY id DESC LIMIT 1`, RecordingIngestKind, recordingMBID).Scan(&jobID); err != nil {
		t.Fatal(err)
	}
	enqueueUnresolvedRecordingArtists(ctx, runtime, client, recordingEntityID)
	if err := runtime.DB.QueryRow(ctx, `SELECT id FROM river_job WHERE kind=$1 AND args->>'provider_id'=$2 ORDER BY id DESC LIMIT 1`, ArtistIngestKind, artistMBID).Scan(&artistJobID); err != nil {
		t.Fatal(err)
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

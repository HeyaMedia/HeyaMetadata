package providercache

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/config"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers"
)

func TestIntegrationConcurrentMissFetchesUpstreamOnce(t *testing.T) {
	if os.Getenv("HEYA_METADATA_INTEGRATION") != "1" {
		t.Skip("set HEYA_METADATA_INTEGRATION=1 to use the local Postgres, Redis, and S3 stack")
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
	cleanupIntegrationArtifacts(t, runtime)
	t.Cleanup(func() { cleanupIntegrationArtifacts(t, runtime) })

	random := make([]byte, 16)
	_, _ = rand.Read(random)
	requestKey := "integration/concurrent/" + hex.EncodeToString(random)
	body := append([]byte(`{"nonce":"`), append([]byte(hex.EncodeToString(random)), []byte(`"}`)...)...)
	template := providers.Payload{Provider: "provider-cache-integration", ProviderNamespace: "test", ProviderRecordID: hex.EncodeToString(random), RequestKey: requestKey}
	retention := providers.RetentionPolicy{Class: "provider_raw_24h", Duration: 24 * time.Hour, ObjectPrefix: "ephemeral/24h"}
	policy := providers.ResponseCachePolicy{ReuseDuration: time.Hour, RedisBodyDuration: time.Minute, MaxRedisBodyBytes: 1024}
	resolver, err := New(runtime, "integration/v1", retention, policy, 0)
	if err != nil {
		t.Fatal(err)
	}
	var fetches atomic.Int32
	fetch := func() (providers.Payload, error) {
		fetches.Add(1)
		time.Sleep(200 * time.Millisecond)
		payload := template
		payload.StatusCode = http.StatusOK
		payload.Headers = http.Header{"Content-Type": {"application/json"}}
		payload.Body = body
		payload.ObservedAt = time.Now().UTC()
		return payload, nil
	}

	start := make(chan struct{})
	results := make([]providers.Payload, 2)
	errors := make([]error, 2)
	var wait sync.WaitGroup
	for index := range results {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			<-start
			results[index], errors[index] = resolver.Resolve(ctx, template, fetch)
		}(index)
	}
	close(start)
	wait.Wait()
	for _, err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	if fetches.Load() != 1 {
		t.Fatalf("upstream fetches: got %d, want 1", fetches.Load())
	}
	if results[0].ObservationID == "" || results[0].ObservationID != results[1].ObservationID {
		t.Fatalf("responses did not reuse one observation: %+v", results)
	}
	if err := runtime.Redis.Set(ctx, "heya:metadata:v1:provider-body:"+results[0].BlobChecksum, "corrupt", time.Minute).Err(); err != nil {
		t.Fatal(err)
	}
	recovered, err := resolver.Resolve(ctx, template, fetch)
	if err != nil {
		t.Fatal(err)
	}
	if fetches.Load() != 1 || !recovered.FromCache {
		t.Fatalf("corrupt Redis body did not recover from S3: fetches=%d cached=%t", fetches.Load(), recovered.FromCache)
	}

	// Simulate lifecycle removing S3 before the pointer is reconciled. The
	// resolver must invalidate the stale entry and safely fetch again.
	fingerprint := providers.RequestFingerprint(template.Provider, requestKey)
	_ = runtime.Redis.Del(ctx, pointerKey(template.Provider, fingerprint), "heya:metadata:v1:provider-body:"+results[0].BlobChecksum).Err()
	objectKey, _ := runtime.Blobs.ContentKeyUnder(retention.ObjectPrefix, results[0].BlobChecksum, ".json.gz")
	if err := runtime.Blobs.Delete(ctx, objectKey); err != nil {
		t.Fatal(err)
	}
	refetched, err := resolver.Resolve(ctx, template, fetch)
	if err != nil {
		t.Fatal(err)
	}
	if fetches.Load() != 2 || refetched.FromCache {
		t.Fatalf("missing S3 body did not fall through to one new fetch: fetches=%d cached=%t", fetches.Load(), refetched.FromCache)
	}

}

func cleanupIntegrationArtifacts(t *testing.T, runtime *platform.Runtime) {
	t.Helper()
	ctx := context.Background()
	rows, err := runtime.DB.Query(ctx, `
		SELECT DISTINCT sb.checksum, sb.object_key
		FROM provider_observations po
		JOIN source_blobs sb ON sb.checksum = po.blob_checksum
		WHERE po.provider = 'provider-cache-integration'`)
	if err != nil {
		t.Errorf("query integration artifacts: %v", err)
		return
	}
	var checksums, objectKeys []string
	for rows.Next() {
		var checksum, objectKey string
		if err := rows.Scan(&checksum, &objectKey); err != nil {
			rows.Close()
			t.Errorf("scan integration artifact: %v", err)
			return
		}
		checksums = append(checksums, checksum)
		objectKeys = append(objectKeys, objectKey)
	}
	rows.Close()
	_, _ = runtime.DB.Exec(ctx, `DELETE FROM provider_observations WHERE provider = 'provider-cache-integration'`)
	for index, checksum := range checksums {
		_, _ = runtime.DB.Exec(ctx, `DELETE FROM source_blobs WHERE checksum = $1 AND NOT EXISTS (SELECT 1 FROM provider_observations WHERE blob_checksum = $1)`, checksum)
		_ = runtime.Blobs.Delete(ctx, objectKeys[index])
		_ = runtime.Redis.Del(ctx, "heya:metadata:v1:provider-body:"+checksum).Err()
	}
	keys, _ := runtime.Redis.Keys(ctx, "heya:metadata:v1:provider-cache:provider-cache-integration:*").Result()
	if len(keys) > 0 {
		_ = runtime.Redis.Del(ctx, keys...).Err()
	}
}

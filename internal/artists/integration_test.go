package artists

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/config"
	artistdomain "github.com/HeyaMedia/HeyaMetadata/internal/domains/artist"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
)

func integrationRuntime(t *testing.T) *platform.Runtime {
	t.Helper()
	if os.Getenv("HEYA_METADATA_INTEGRATION") != "1" {
		t.Skip("set HEYA_METADATA_INTEGRATION=1 to use the local platform stack")
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
	return runtime
}

func testObservation(t *testing.T, runtime *platform.Runtime, provider, value string) string {
	t.Helper()
	var id string
	err := runtime.DB.QueryRow(context.Background(), `INSERT INTO provider_observations (provider,provider_namespace,provider_record_id,request_key,response_status,observed_at,normalizer_version,retention_class) VALUES ($1,'artist',$2,$3,200,now(),$4,'provider_raw_48h') RETURNING id`, provider, value, "integration/"+value+"/"+fmt.Sprint(time.Now().UnixNano()), provider+"-artist/integration").Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = runtime.DB.Exec(context.Background(), `DELETE FROM normalized_records WHERE primary_observation_id=$1 AND entity_id IS NULL`, id)
		_, _ = runtime.DB.Exec(context.Background(), `DELETE FROM provider_observations WHERE id=$1`, id)
	})
	return id
}

func cleanupArtist(t *testing.T, runtime *platform.Runtime, entityIDs []string, observationIDs []string) {
	t.Helper()
	ctx := context.Background()
	for _, id := range entityIDs {
		_, _ = runtime.DB.Exec(ctx, `DELETE FROM change_log WHERE entity_id=$1`, id)
		_, _ = runtime.DB.Exec(ctx, `DELETE FROM change_outbox WHERE entity_id=$1`, id)
		_, _ = runtime.DB.Exec(ctx, `DELETE FROM provider_refresh_states WHERE entity_id=$1`, id)
		_, _ = runtime.DB.Exec(ctx, `DELETE FROM search_names WHERE entity_id=$1`, id)
		_, _ = runtime.DB.Exec(ctx, `DELETE FROM search_entities WHERE entity_id=$1`, id)
		_, _ = runtime.DB.Exec(ctx, `DELETE FROM api_document_provenance WHERE entity_id=$1`, id)
		_, _ = runtime.DB.Exec(ctx, `DELETE FROM api_documents WHERE entity_id=$1`, id)
		_, _ = runtime.DB.Exec(ctx, `DELETE FROM image_candidates WHERE entity_id=$1`, id)
		_, _ = runtime.DB.Exec(ctx, `DELETE FROM canonical_artists WHERE entity_id=$1`, id)
		_, _ = runtime.DB.Exec(ctx, `DELETE FROM normalized_records WHERE entity_id=$1`, id)
		_, _ = runtime.DB.Exec(ctx, `DELETE FROM external_id_claims WHERE entity_id=$1`, id)
		_, _ = runtime.DB.Exec(ctx, `DELETE FROM entity_access_stats WHERE entity_id=$1`, id)
		_, _ = runtime.DB.Exec(ctx, `DELETE FROM entity_slugs WHERE entity_id=$1`, id)
		_, _ = runtime.DB.Exec(ctx, `DELETE FROM entities WHERE id=$1`, id)
		_ = runtime.Redis.Del(ctx, "heya:metadata:v1:api:entity:"+id+":detail").Err()
	}
	for _, id := range observationIDs {
		_, _ = runtime.DB.Exec(ctx, `DELETE FROM provider_observations WHERE id=$1`, id)
	}
}

func TestIntegrationArtistMergeIsIdempotentAndResolvable(t *testing.T) {
	runtime := integrationRuntime(t)
	ctx := context.Background()
	suffix := fmt.Sprint(time.Now().UnixNano())
	mbid := "00000000-0000-4000-8000-" + suffix[len(suffix)-12:]
	observation := testObservation(t, runtime, "musicbrainz", mbid)
	record := artistdomain.NormalizedRecordV1{ProviderRecord: artistdomain.ProviderRecord{Provider: "musicbrainz", Namespace: "artist", Value: mbid, PrimaryObservationID: observation, ObservedAt: time.Now().UTC(), NormalizerVersion: "integration", SchemaVersion: 1}, IdentityCandidates: []artistdomain.IdentityCandidate{{Provider: "musicbrainz", Namespace: "artist", NormalizedValue: mbid, Confidence: 1, Evidence: "integration"}}, Names: []artistdomain.Name{{Value: "Integration Artist " + suffix, Type: "display", Primary: true}}}
	service := NewService(runtime)
	normalizedID, err := service.recordNormalized(ctx, record)
	if err != nil {
		t.Fatal(err)
	}
	first, err := service.merge(ctx, []string{normalizedID}, []artistdomain.NormalizedRecordV1{record}, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanupArtist(t, runtime, []string{first.EntityID}, []string{observation}) })
	second, err := service.merge(ctx, []string{normalizedID}, []artistdomain.NormalizedRecordV1{record}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if second.EntityID != first.EntityID || second.ProjectionVersion != first.ProjectionVersion+1 {
		t.Fatalf("merge was not idempotent: first=%+v second=%+v", first, second)
	}
	resolved, err := service.Resolve(ctx, "musicbrainz", "artist", strings.ToUpper(mbid))
	if err != nil || resolved != first.EntityID {
		t.Fatalf("resolve: id=%s err=%v", resolved, err)
	}
}

func TestIntegrationArtistMergeQuarantinesConflictingStrongClaims(t *testing.T) {
	runtime := integrationRuntime(t)
	ctx := context.Background()
	suffix := fmt.Sprint(time.Now().UnixNano())
	observation := testObservation(t, runtime, "musicbrainz", "conflict-"+suffix)
	var left, right string
	if err := runtime.DB.QueryRow(ctx, `INSERT INTO entities (kind,slug) VALUES ('artist',$1) RETURNING id`, `integration-left-`+suffix).Scan(&left); err != nil {
		t.Fatal(err)
	}
	if err := runtime.DB.QueryRow(ctx, `INSERT INTO entities (kind,slug) VALUES ('artist',$1) RETURNING id`, `integration-right-`+suffix).Scan(&right); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanupArtist(t, runtime, []string{left, right}, []string{observation}) })
	for _, claim := range []struct{ entity, provider, value string }{{left, "apple", "apple-" + suffix}, {right, "discogs", "discogs-" + suffix}} {
		if _, err := runtime.DB.Exec(ctx, `INSERT INTO external_id_claims (entity_id,entity_kind,provider,namespace,normalized_value,state,confidence,source_observation_id,first_observed_at,last_observed_at) VALUES ($1,'artist',$2,'artist',$3,'accepted',1,$4,now(),now())`, claim.entity, claim.provider, claim.value, observation); err != nil {
			t.Fatal(err)
		}
	}
	record := artistdomain.NormalizedRecordV1{ProviderRecord: artistdomain.ProviderRecord{Provider: "musicbrainz", Namespace: "artist", Value: "conflict-" + suffix, PrimaryObservationID: observation, ObservedAt: time.Now().UTC(), NormalizerVersion: "integration", SchemaVersion: 1}, IdentityCandidates: []artistdomain.IdentityCandidate{{Provider: "apple", Namespace: "artist", NormalizedValue: "apple-" + suffix, Confidence: 1}, {Provider: "discogs", Namespace: "artist", NormalizedValue: "discogs-" + suffix, Confidence: 1}}, Names: []artistdomain.Name{{Value: "Conflicting Artist", Type: "display", Primary: true}}}
	service := NewService(runtime)
	normalizedID, err := service.recordNormalized(ctx, record)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.merge(ctx, []string{normalizedID}, []artistdomain.NormalizedRecordV1{record}, 0); err == nil || !strings.Contains(err.Error(), "multiple canonical artists") {
		t.Fatalf("expected quarantined conflict, got %v", err)
	}
	var conflicts int
	if err := runtime.DB.QueryRow(ctx, `SELECT count(*) FROM external_id_conflicts WHERE entity_kind='artist' AND normalized_record_id=$1 AND state='open'`, normalizedID).Scan(&conflicts); err != nil {
		t.Fatal(err)
	}
	if conflicts != 1 {
		t.Fatalf("conflict rows: %d", conflicts)
	}
	_, _ = runtime.DB.Exec(ctx, `DELETE FROM external_id_conflicts WHERE normalized_record_id=$1`, normalizedID)
	_, _ = runtime.DB.Exec(ctx, `DELETE FROM normalized_records WHERE id=$1`, normalizedID)
}

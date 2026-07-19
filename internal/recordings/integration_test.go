package recordings

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/config"
	releasedomain "github.com/HeyaMedia/HeyaMetadata/internal/domains/release"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/google/uuid"
)

func recordingIntegrationRuntime(t *testing.T) *platform.Runtime {
	t.Helper()
	if os.Getenv("HEYA_METADATA_INTEGRATION") != "1" {
		t.Skip("set HEYA_METADATA_INTEGRATION=1 to use the local platform stack")
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := platform.Open(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(runtime.Close)
	return runtime
}

func TestIntegrationRecordingPersistsMaterializedArtistCreditRelation(t *testing.T) {
	runtime := recordingIntegrationRuntime(t)
	ctx := context.Background()
	recordingMBID := uuid.NewString()
	artistMBID := uuid.NewString()
	observedAt := time.Now().UTC()

	var recordingObservation, artistObservation string
	if err := runtime.DB.QueryRow(ctx, `INSERT INTO provider_observations(provider,provider_namespace,provider_record_id,request_key,response_status,observed_at,normalizer_version,retention_class)VALUES('musicbrainz','recording',$1,$2,200,$3,'integration','provider_raw_48h')RETURNING id`, recordingMBID, "integration-recording-"+recordingMBID, observedAt).Scan(&recordingObservation); err != nil {
		t.Fatal(err)
	}
	if err := runtime.DB.QueryRow(ctx, `INSERT INTO provider_observations(provider,provider_namespace,provider_record_id,request_key,response_status,observed_at,normalizer_version,retention_class)VALUES('musicbrainz','artist',$1,$2,200,$3,'integration','provider_raw_48h')RETURNING id`, artistMBID, "integration-artist-"+artistMBID, observedAt).Scan(&artistObservation); err != nil {
		t.Fatal(err)
	}

	var artistEntityID string
	materialize := WithArtistCreditMaterializer(func(ctx context.Context, mbid string) error {
		if err := runtime.DB.QueryRow(ctx, `INSERT INTO entities(kind,slug)VALUES('artist',$1)RETURNING id`, "recording-credit-"+mbid).Scan(&artistEntityID); err != nil {
			return err
		}
		if _, err := runtime.DB.Exec(ctx, `INSERT INTO entity_slugs(entity_id,kind,slug)VALUES($1,'artist',$2)`, artistEntityID, "recording-credit-"+mbid); err != nil {
			return err
		}
		_, err := runtime.DB.Exec(ctx, `INSERT INTO external_id_claims(entity_id,entity_kind,provider,namespace,normalized_value,state,confidence,source_observation_id,first_observed_at,last_observed_at)VALUES($1,'artist','musicbrainz','artist',$2,'accepted',1,$3,$4,$4)`, artistEntityID, mbid, artistObservation, observedAt)
		return err
	})
	service := NewService(runtime, materialize)
	credits := []releasedomain.ArtistCredit{{Position: 0, Name: "Integration Artist", ArtistName: "Integration Artist", ArtistProvider: "musicbrainz", ArtistNamespace: "artist", ArtistID: artistMBID}}
	if err := service.materializeArtistCredits(ctx, credits); err != nil {
		t.Fatal(err)
	}
	record := releasedomain.NormalizedRecording{
		ProviderRecord: releasedomain.ProviderRecord{Provider: "musicbrainz", Namespace: "recording", Value: recordingMBID, PrimaryObservationID: recordingObservation, NormalizerVersion: "integration", ObservedAt: observedAt, SchemaVersion: 1},
		Recording:      releasedomain.Recording{Provider: "musicbrainz", Namespace: "recording", ProviderID: recordingMBID, Title: "Integration Recording", ArtistCredits: credits},
	}
	result, err := service.persist(ctx, record, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, entityID := range []string{result.EntityID, artistEntityID} {
			_, _ = runtime.DB.Exec(context.Background(), `DELETE FROM change_log WHERE entity_id=$1`, entityID)
			_, _ = runtime.DB.Exec(context.Background(), `DELETE FROM change_outbox WHERE entity_id=$1`, entityID)
			_, _ = runtime.DB.Exec(context.Background(), `DELETE FROM provider_refresh_states WHERE entity_id=$1`, entityID)
			_, _ = runtime.DB.Exec(context.Background(), `DELETE FROM search_names WHERE entity_id=$1`, entityID)
			_, _ = runtime.DB.Exec(context.Background(), `DELETE FROM search_entities WHERE entity_id=$1`, entityID)
			_, _ = runtime.DB.Exec(context.Background(), `DELETE FROM api_documents WHERE entity_id=$1`, entityID)
			_, _ = runtime.DB.Exec(context.Background(), `DELETE FROM canonical_recordings WHERE entity_id=$1`, entityID)
			_, _ = runtime.DB.Exec(context.Background(), `DELETE FROM normalized_records WHERE entity_id=$1`, entityID)
			_, _ = runtime.DB.Exec(context.Background(), `DELETE FROM external_id_claims WHERE entity_id=$1`, entityID)
			_, _ = runtime.DB.Exec(context.Background(), `DELETE FROM entity_slugs WHERE entity_id=$1`, entityID)
			_, _ = runtime.DB.Exec(context.Background(), `DELETE FROM entities WHERE id=$1`, entityID)
		}
		_, _ = runtime.DB.Exec(context.Background(), `DELETE FROM provider_observations WHERE id=ANY($1::uuid[])`, []string{recordingObservation, artistObservation})
	})

	if len(result.Detail.Data.ArtistCredits) != 1 || result.Detail.Data.ArtistCredits[0].ArtistEntityID != artistEntityID || result.Detail.Data.ArtistCredits[0].ResolutionState != "materialized" {
		t.Fatalf("recording credits = %+v", result.Detail.Data.ArtistCredits)
	}
	creditSources := result.Detail.Provenance["credits"]
	if len(creditSources) != 1 || creditSources[0].Provider != "musicbrainz" || creditSources[0].ObservationID != recordingObservation {
		t.Fatalf("recording credit provenance = %+v", creditSources)
	}
	var relationTarget string
	if err := runtime.DB.QueryRow(ctx, `SELECT target_entity_id::text FROM entity_relations WHERE source_entity_id=$1 AND relation_type='artist_credit' AND provider='musicbrainz' AND namespace='artist' AND provider_value=$2 AND state='accepted'`, result.EntityID, artistMBID).Scan(&relationTarget); err != nil {
		t.Fatal(err)
	}
	if relationTarget != artistEntityID {
		t.Fatalf("artist-credit relation target = %q, want %q", relationTarget, artistEntityID)
	}
	var emitted bool
	if err := runtime.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM change_outbox WHERE entity_id=$1 AND projection_version=$2 AND changed_scopes @> ARRAY['credits','relations'])`, result.EntityID, result.ProjectionVersion).Scan(&emitted); err != nil {
		t.Fatal(err)
	}
	if !emitted {
		t.Fatal("recording credit projection did not emit credits/relations scopes")
	}
}

func TestIntegrationOlderRecordingObservationCannotRegressCanonicalSnapshot(t *testing.T) {
	runtime := recordingIntegrationRuntime(t)
	ctx := context.Background()
	recordingMBID := uuid.NewString()
	newerArtistMBID := uuid.NewString()
	olderArtistMBID := uuid.NewString()
	newerWorkMBID := uuid.NewString()
	olderWorkMBID := uuid.NewString()
	newerISRC := "USAAA2600001"
	olderISRC := "USAAA2500001"
	newerAt := time.Now().UTC()
	olderAt := newerAt.Add(-time.Hour)

	observationIDs := []string{}
	insertObservation := func(observedAt time.Time, suffix string) string {
		t.Helper()
		var observationID string
		if err := runtime.DB.QueryRow(ctx, `INSERT INTO provider_observations(provider,provider_namespace,provider_record_id,request_key,response_status,observed_at,normalizer_version,retention_class)VALUES('musicbrainz','recording',$1,$2,200,$3,'integration','provider_raw_48h')RETURNING id`, recordingMBID, "integration-recording-snapshot-"+suffix+"-"+recordingMBID, observedAt).Scan(&observationID); err != nil {
			t.Fatal(err)
		}
		observationIDs = append(observationIDs, observationID)
		return observationID
	}
	newerObservation := insertObservation(newerAt, "newer")
	olderObservation := insertObservation(olderAt, "older")
	makeRecord := func(observationID string, observedAt time.Time, artistID, artistName, workID, isrc string) releasedomain.NormalizedRecording {
		return releasedomain.NormalizedRecording{
			ProviderRecord: releasedomain.ProviderRecord{Provider: "musicbrainz", Namespace: "recording", Value: recordingMBID, PrimaryObservationID: observationID, NormalizerVersion: "integration", ObservedAt: observedAt, SchemaVersion: 1},
			Recording: releasedomain.Recording{
				Provider: "musicbrainz", Namespace: "recording", ProviderID: recordingMBID, Title: "Monotonic Recording",
				ISRCs:         []string{isrc},
				ArtistCredits: []releasedomain.ArtistCredit{{Position: 0, Name: artistName, ArtistName: artistName, ArtistProvider: "musicbrainz", ArtistNamespace: "artist", ArtistID: artistID}},
			},
			WorkRelations: []releasedomain.WorkRelation{{ProviderID: workID, Title: "Monotonic Work", Type: "performance"}},
		}
	}
	service := NewService(runtime)
	newer, err := service.persist(ctx, makeRecord(newerObservation, newerAt, newerArtistMBID, "Newer Artist", newerWorkMBID, newerISRC), 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		entityID := newer.EntityID
		for _, statement := range []string{
			`DELETE FROM change_log WHERE entity_id=$1`,
			`DELETE FROM change_outbox WHERE entity_id=$1`,
			`DELETE FROM provider_refresh_states WHERE entity_id=$1`,
			`DELETE FROM entity_relations WHERE source_entity_id=$1 OR target_entity_id=$1`,
			`DELETE FROM search_names WHERE entity_id=$1`,
			`DELETE FROM search_entities WHERE entity_id=$1`,
			`DELETE FROM api_documents WHERE entity_id=$1`,
			`DELETE FROM canonical_recordings WHERE entity_id=$1`,
			`DELETE FROM normalized_records WHERE entity_id=$1`,
			`DELETE FROM external_id_claims WHERE entity_id=$1`,
			`DELETE FROM entity_slugs WHERE entity_id=$1`,
			`DELETE FROM entities WHERE id=$1`,
		} {
			_, _ = runtime.DB.Exec(context.Background(), statement, entityID)
		}
		_, _ = runtime.DB.Exec(context.Background(), `DELETE FROM provider_observations WHERE id=ANY($1::uuid[])`, observationIDs)
	})

	var outboxBefore int
	if err := runtime.DB.QueryRow(ctx, `SELECT count(*) FROM change_outbox WHERE entity_id=$1`, newer.EntityID).Scan(&outboxBefore); err != nil {
		t.Fatal(err)
	}
	older, err := service.persist(ctx, makeRecord(olderObservation, olderAt, olderArtistMBID, "Older Artist", olderWorkMBID, olderISRC), 0)
	if err != nil {
		t.Fatal(err)
	}
	if older.ProjectionVersion != newer.ProjectionVersion {
		t.Fatalf("stale snapshot advanced projection %d -> %d", newer.ProjectionVersion, older.ProjectionVersion)
	}
	if len(older.Detail.Data.ArtistCredits) != 1 || older.Detail.Data.ArtistCredits[0].ArtistID != newerArtistMBID {
		t.Fatalf("older observation replaced public credits: %+v", older.Detail.Data.ArtistCredits)
	}
	creditSources := older.Detail.Provenance["credits"]
	if len(creditSources) != 1 || creditSources[0].ObservationID != newerObservation {
		t.Fatalf("credit provenance regressed: %+v", creditSources)
	}
	var acceptedNewer, acceptedOlder bool
	if err := runtime.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM entity_relations WHERE source_entity_id=$1 AND relation_type='artist_credit' AND provider_value=$2 AND state='accepted'),EXISTS(SELECT 1 FROM entity_relations WHERE source_entity_id=$1 AND relation_type='artist_credit' AND provider_value=$3 AND state='accepted')`, newer.EntityID, newerArtistMBID, olderArtistMBID).Scan(&acceptedNewer, &acceptedOlder); err != nil {
		t.Fatal(err)
	}
	if !acceptedNewer || acceptedOlder {
		t.Fatalf("accepted newer=%v older=%v", acceptedNewer, acceptedOlder)
	}
	var acceptedNewerWork, acceptedOlderWork, proposedNewerISRC, proposedOlderISRC bool
	if err := runtime.DB.QueryRow(ctx, `
		SELECT
			EXISTS(SELECT 1 FROM entity_relations WHERE source_entity_id=$1 AND relation_type='performance_of' AND provider_value=$2 AND state='accepted'),
			EXISTS(SELECT 1 FROM entity_relations WHERE source_entity_id=$1 AND relation_type='performance_of' AND provider_value=$3 AND state='accepted'),
			EXISTS(SELECT 1 FROM external_id_claims WHERE entity_id=$1 AND provider='isrc' AND normalized_value=$4 AND state='proposed'),
			EXISTS(SELECT 1 FROM external_id_claims WHERE entity_id=$1 AND provider='isrc' AND normalized_value=$5 AND state='proposed')`, newer.EntityID, newerWorkMBID, olderWorkMBID, newerISRC, olderISRC).Scan(&acceptedNewerWork, &acceptedOlderWork, &proposedNewerISRC, &proposedOlderISRC); err != nil {
		t.Fatal(err)
	}
	if !acceptedNewerWork || acceptedOlderWork || !proposedNewerISRC || proposedOlderISRC {
		t.Fatalf("work/claim snapshot regressed: newer_work=%v older_work=%v newer_isrc=%v older_isrc=%v", acceptedNewerWork, acceptedOlderWork, proposedNewerISRC, proposedOlderISRC)
	}
	var refreshObservation string
	if err := runtime.DB.QueryRow(ctx, `SELECT last_observation_id::text FROM provider_refresh_states WHERE entity_id=$1 AND provider='musicbrainz'`, newer.EntityID).Scan(&refreshObservation); err != nil {
		t.Fatal(err)
	}
	if refreshObservation != newerObservation {
		t.Fatalf("provider refresh state regressed to %s", refreshObservation)
	}
	var outboxAfter int
	if err := runtime.DB.QueryRow(ctx, `SELECT count(*) FROM change_outbox WHERE entity_id=$1`, newer.EntityID).Scan(&outboxAfter); err != nil {
		t.Fatal(err)
	}
	if outboxAfter != outboxBefore {
		t.Fatalf("stale snapshot emitted change outbox row: before=%d after=%d", outboxBefore, outboxAfter)
	}
}

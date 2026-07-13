package episodic

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/config"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
)

func TestIntegrationChildResourcesKeepIdentityAndOpaqueArtworkAcrossRefresh(t *testing.T) {
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

	suffix := fmt.Sprint(time.Now().UnixNano())
	showProviderID := "integration-" + suffix
	var observationID, supplementalObservationID string
	if err := runtime.DB.QueryRow(ctx, `INSERT INTO provider_observations(provider,provider_namespace,provider_record_id,request_key,response_status,observed_at,normalizer_version,retention_class)VALUES('tvmaze','show',$1,$2,200,now(),'integration','provider_raw_48h')RETURNING id`, showProviderID, "integration/episodic/"+suffix).Scan(&observationID); err != nil {
		t.Fatal(err)
	}
	if err := runtime.DB.QueryRow(ctx, `INSERT INTO provider_observations(provider,provider_namespace,provider_record_id,request_key,response_status,observed_at,normalizer_version,retention_class)VALUES('tmdb','tv',$1,$2,200,now(),'integration','provider_raw_48h')RETURNING id`, showProviderID, "integration/episodic/tmdb/"+suffix).Scan(&supplementalObservationID); err != nil {
		t.Fatal(err)
	}

	observedAt := time.Now().UTC()
	record := NormalizedRecord{
		SchemaVersion: 1, Kind: "tv_show", Provider: "tvmaze", Namespace: "show", ProviderID: showProviderID,
		PrimaryObservationID: observationID, ObservedAt: observedAt, NormalizerVersion: "integration",
		Titles:      []Title{{Value: "Integration Show " + suffix, Language: "en", Type: "main"}},
		ExternalIDs: []ExternalID{{Provider: "tvmaze", Namespace: "show", Value: showProviderID}},
		Seasons:     []Season{{ProviderID: "season-1-" + suffix, Number: 1, Name: "Season 1", Titles: []Title{{Value: "Season 1", Language: "en", Type: "display"}}, ExternalIDs: []ExternalID{{Provider: "tvmaze", Namespace: "season", Value: "season-1-" + suffix}}, Images: []Image{{Provider: "tvmaze", ProviderID: "season-art-" + suffix, URL: "https://example.test/season.jpg", Class: "poster", Language: "en"}}}},
		Episodes:    []Episode{{ProviderID: "episode-1-" + suffix, ExternalIDs: []ExternalID{{Provider: "tvmaze", Namespace: "episode", Value: "episode-1-" + suffix}}, Titles: []Title{{Value: "Pilot", Language: "en", Type: "main"}}, Overviews: []Text{{Value: "The story begins.", Language: "en", Type: "overview"}}, Numbers: []EpisodeNumber{{Scheme: "tvmaze", Season: 1, Number: 1, Provider: "tvmaze"}, {Scheme: "aired", Season: 1, Number: 1, Provider: "tvmaze"}}, EpisodeType: "regular", AirDate: "2026-01-01", Images: []Image{{Provider: "tvmaze", ProviderID: "episode-art-" + suffix, URL: "https://example.test/episode.jpg", Class: "still", Language: "en"}}}},
	}
	def := Definition{Kind: "tv_show", Provider: "tvmaze", Namespace: "show", NormalizerVersion: "integration", MergeVersion: "integration"}
	supplement := NormalizedRecord{SchemaVersion: 1, Kind: "tv_show", Provider: "tmdb", Namespace: "tv", ProviderID: "tmdb-" + suffix, PrimaryObservationID: supplementalObservationID, ObservedAt: observedAt, NormalizerVersion: "integration", ExternalIDs: []ExternalID{{Provider: "tmdb", Namespace: "tv", Value: "tmdb-" + suffix}}, Episodes: []Episode{{ProviderID: "tmdb-episode-" + suffix, ExternalIDs: []ExternalID{{Provider: "tmdb", Namespace: "episode", Value: "tmdb-episode-" + suffix}}, Titles: []Title{{Value: "Pilot", Language: "en", Type: "main"}}, Numbers: []EpisodeNumber{{Scheme: "aired", Season: 1, Number: 1, Provider: "tmdb"}, {Scheme: "tmdb", Season: 1, Number: 1, Provider: "tmdb"}}, EpisodeType: "regular", AirDate: "2026-01-01", Ratings: []Rating{{System: "tmdb", Value: 8, ScaleMin: 0, ScaleMax: 10, Votes: 10}}}}}
	first, err := PersistMany(ctx, runtime, def, []NormalizedRecord{record, supplement}, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupIntegrationEpisodic(runtime, first.EntityID, []string{observationID, supplementalObservationID})
	})
	seasonID := first.Document.Data.Seasons[0].ID
	episodeID := first.Document.Data.Episodes[0].ID
	seasonImageID := first.Document.Data.Seasons[0].Images[0].ID
	episodeImageID := first.Document.Data.Episodes[0].Images[0].ID

	record.Episodes[0].Numbers[0], record.Episodes[0].Numbers[1] = record.Episodes[0].Numbers[1], record.Episodes[0].Numbers[0]
	second, err := Persist(ctx, runtime, def, record, 0)
	if err != nil {
		t.Fatal(err)
	}
	if second.EntityID != first.EntityID || second.Document.Data.Seasons[0].ID != seasonID || second.Document.Data.Episodes[0].ID != episodeID {
		t.Fatalf("child identity changed: first=%+v second=%+v", first.Document.Data, second.Document.Data)
	}
	if len(second.Document.Data.Episodes[0].Ratings) != 1 || second.Document.Freshness.Providers["tmdb"].State != "stale" {
		t.Fatalf("last successful supplement was not retained as stale evidence: episode=%+v freshness=%+v", second.Document.Data.Episodes[0], second.Document.Freshness.Providers)
	}
	if second.Document.Data.Seasons[0].Images[0].ID != seasonImageID || second.Document.Data.Episodes[0].Images[0].ID != episodeImageID || second.Document.Data.Seasons[0].Images[0].URL != "" || second.Document.Data.Episodes[0].Images[0].URL != "" {
		t.Fatalf("child artwork was not stable and opaque: seasons=%+v episodes=%+v", second.Document.Data.Seasons, second.Document.Data.Episodes)
	}

	season, err := SeasonDetail(ctx, runtime, seasonID)
	if err != nil || season.Show.EntityID != first.EntityID || len(season.Episodes) != 1 || season.Episodes[0].ID != episodeID {
		t.Fatalf("season resource=%+v err=%v", season, err)
	}
	episode, err := EpisodeDetail(ctx, runtime, episodeID)
	if err != nil || episode.Show.EntityID != first.EntityID || episode.Data.SeasonID != seasonID || len(episode.Data.ExternalIDs) != 2 {
		t.Fatalf("episode resource=%+v err=%v", episode, err)
	}
	for _, owner := range []struct{ imageID, scope, resourceID string }{{seasonImageID, "season", seasonID}, {episodeImageID, "episode", episodeID}} {
		var scope, resourceID string
		if err := runtime.DB.QueryRow(ctx, `SELECT ownership_scope,owner_resource_id::text FROM image_candidates WHERE id=$1`, owner.imageID).Scan(&scope, &resourceID); err != nil {
			t.Fatal(err)
		}
		if scope != owner.scope || resourceID != owner.resourceID {
			t.Fatalf("image %s owner=%s/%s, want %s/%s", owner.imageID, scope, resourceID, owner.scope, owner.resourceID)
		}
	}
}

func cleanupIntegrationEpisodic(runtime *platform.Runtime, entityID string, observationIDs []string) {
	ctx := context.Background()
	for _, statement := range []string{
		`DELETE FROM change_log WHERE entity_id=$1`,
		`DELETE FROM change_outbox WHERE entity_id=$1`,
		`DELETE FROM provider_refresh_states WHERE entity_id=$1`,
		`DELETE FROM entity_credit_projections WHERE entity_id=$1`,
		`DELETE FROM entity_rating_projections WHERE entity_id=$1`,
		`DELETE FROM search_names WHERE entity_id=$1`,
		`DELETE FROM search_entities WHERE entity_id=$1`,
		`DELETE FROM api_document_provenance WHERE entity_id=$1`,
		`DELETE FROM api_documents WHERE entity_id=$1`,
		`DELETE FROM image_candidates WHERE entity_id=$1`,
		`DELETE FROM episodic_episodes WHERE show_entity_id=$1`,
		`DELETE FROM episodic_seasons WHERE show_entity_id=$1`,
		`DELETE FROM canonical_tv_shows WHERE entity_id=$1`,
		`DELETE FROM normalized_records WHERE entity_id=$1`,
		`DELETE FROM external_id_claims WHERE entity_id=$1`,
		`DELETE FROM entity_access_stats WHERE entity_id=$1`,
		`DELETE FROM entity_slugs WHERE entity_id=$1`,
		`DELETE FROM entities WHERE id=$1`,
	} {
		_, _ = runtime.DB.Exec(ctx, statement, entityID)
	}
	for _, observationID := range observationIDs {
		_, _ = runtime.DB.Exec(ctx, `DELETE FROM provider_observations WHERE id=$1`, observationID)
	}
}

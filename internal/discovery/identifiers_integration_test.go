package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/config"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/HeyaMedia/HeyaMetadata/internal/providercredentials"
)

type tvIdentifierFixture struct {
	TMDB   int64
	TVDB   int64
	TVMaze int64
	IMDb   string
	Title  string
}

func TestIntegrationKnownIdentifierAgreementAndConflictStayCanonical(t *testing.T) {
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
	ids := make([]string, 2)
	for index, title := range []string{"Canonical fixture A", "Canonical fixture B"} {
		slug := fmt.Sprintf("provider-transparent-%s-%d", suffix, index)
		if err := runtime.DB.QueryRow(ctx, `INSERT INTO entities(kind,slug,canonical_version)VALUES('movie',$1,1)RETURNING id::text`, slug).Scan(&ids[index]); err != nil {
			t.Fatal(err)
		}
		if _, err := runtime.DB.Exec(ctx, `INSERT INTO search_entities(entity_id,kind,slug,display_title,summary,projection_version)VALUES($1::uuid,'movie',$2,$3::text,jsonb_build_object('id',($1::uuid)::text,'kind','movie','display',jsonb_build_object('title',$3::text)),1)`, ids[index], slug, title); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		_, _ = runtime.DB.Exec(context.Background(), `DELETE FROM discovery_runs WHERE request->>'query' LIKE $1`, "integration-"+suffix+"%")
		_, _ = runtime.DB.Exec(context.Background(), `DELETE FROM search_entities WHERE entity_id=ANY($1::uuid[])`, ids)
		_, _ = runtime.DB.Exec(context.Background(), `DELETE FROM external_id_claims WHERE entity_id=ANY($1::uuid[])`, ids)
		_, _ = runtime.DB.Exec(context.Background(), `DELETE FROM entities WHERE id=ANY($1::uuid[])`, ids)
	})

	imdbA := "tt" + suffix
	tmdbNumber, err := strconv.ParseInt(suffix, 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	tmdbA, tmdbB := suffix, strconv.FormatInt(tmdbNumber+1, 10)
	for _, claim := range []struct{ entityID, provider, namespace, value string }{
		{ids[0], "imdb", "title", imdbA},
		{ids[0], "tmdb", "movie", tmdbA},
		{ids[1], "tmdb", "movie", tmdbB},
	} {
		if _, err := runtime.DB.Exec(ctx, `INSERT INTO external_id_claims(entity_id,entity_kind,provider,namespace,normalized_value,state,first_observed_at,last_observed_at)VALUES($1,'movie',$2,$3,$4,'accepted',now(),now())`, claim.entityID, claim.provider, claim.namespace, claim.value); err != nil {
			t.Fatal(err)
		}
	}

	service := NewService(runtime)
	agreement := Request{Kind: KindMovie, Query: "integration-" + suffix + "-agreement", Identifiers: []Identifier{{Scheme: "tmdb", Value: tmdbA}, {Scheme: "imdb", Value: imdbA}}}
	result, handled, err := service.ResolveKnownIdentifiers(ctx, agreement)
	if err != nil || !handled || result.EntityID != ids[0] || len(result.Candidates) != 0 {
		t.Fatalf("agreement result=%+v handled=%v err=%v", result, handled, err)
	}
	if result.IdentifierEvidence[1].Outcome != "corroborating" {
		t.Fatalf("agreement evidence=%+v", result.IdentifierEvidence)
	}

	conflict := Request{Kind: KindMovie, Query: "integration-" + suffix + "-conflict", Identifiers: []Identifier{{Scheme: "imdb", Value: imdbA}, {Scheme: "tmdb", Value: tmdbB}}}
	run, err := EnsureRun(ctx, runtime, conflict)
	if err != nil {
		t.Fatal(err)
	}
	result, handled, err = service.ResolveKnownIdentifiers(ctx, conflict)
	if err != nil || !handled || result.Status != "needs_selection" || len(result.Candidates) != 2 {
		t.Fatalf("conflict result=%+v handled=%v err=%v", result, handled, err)
	}
	if err := Complete(ctx, runtime, run.RequestHash, result); err != nil {
		t.Fatal(err)
	}
	completed, err := GetRun(ctx, runtime, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range completed.Result.Candidates {
		if candidate.CandidateRef == "" {
			t.Fatal("conflicting candidate has no opaque reference")
		}
		body, _ := json.Marshal(candidate)
		for _, forbidden := range []string{"provider", "namespace", "resolution", "identity"} {
			if strings.Contains(string(body), `"`+forbidden+`"`) {
				t.Fatalf("candidate leaked %s: %s", forbidden, body)
			}
		}
		selection, err := ResolveCandidate(ctx, runtime, candidate.CandidateRef)
		if err != nil || selection.Provider != "heya" || selection.Namespace != "entity" {
			t.Fatalf("private selection=%+v err=%v", selection, err)
		}
	}
}

func TestIntegrationFreshTVIdentifierRoutesAndMixedEvidence(t *testing.T) {
	if os.Getenv("HEYA_METADATA_INTEGRATION") != "1" {
		t.Skip("set HEYA_METADATA_INTEGRATION=1 to use the local platform stack")
	}
	ctx := context.Background()
	base := time.Now().UnixNano()%800_000_000 + 100_000_000
	suffix := strconv.FormatInt(base, 10)
	fixtures := []tvIdentifierFixture{
		{TMDB: base + 10, TVDB: base + 11, TVMaze: base + 12, IMDb: "tt91" + suffix, Title: "Fresh TMDB fixture " + suffix},
		{TMDB: base + 20, TVDB: base + 21, TVMaze: base + 22, IMDb: "tt92" + suffix, Title: "Fresh IMDb fixture " + suffix},
		{TMDB: base + 30, TVDB: base + 31, TVMaze: base + 32, IMDb: "tt93" + suffix, Title: "Fresh TVDB fixture " + suffix},
		{TMDB: base + 40, TVDB: base + 41, TVMaze: base + 42, IMDb: "tt94" + suffix, Title: "Conflicting fixture " + suffix},
	}
	server := httptest.NewServer(tvIdentifierFixtureHandler(fixtures))
	t.Cleanup(server.Close)
	blobServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.Method {
		case http.MethodPut:
			response.WriteHeader(http.StatusOK)
		case http.MethodDelete:
			response.WriteHeader(http.StatusNoContent)
		default:
			response.WriteHeader(http.StatusNotFound)
			_, _ = response.Write([]byte(`<Error><Code>NoSuchKey</Code></Error>`))
		}
	}))
	t.Cleanup(blobServer.Close)

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg.S3.Endpoint = blobServer.URL
	cfg.S3.Bucket = "fixture"
	cfg.S3.Prefix = "discovery"
	cfg.S3.AccessKeyID = "fixture-access"
	cfg.S3.SecretAccessKey = "fixture-secret"
	runtime, err := platform.Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(runtime.Close)
	runtime.Config.Providers.TMDB.BaseURL = server.URL
	runtime.Config.Providers.TMDB.Token = "fixture-token"
	runtime.Config.Providers.TVMaze.BaseURL = server.URL
	runtime.Config.Providers.TVMaze.RequestsPerSecond = 1000
	runtime.Config.Providers.TVDB.APIKey = ""
	runtime.Config.Providers.Fanart.APIKey = ""

	service := NewService(runtime)
	entityIDs := []string{}
	t.Cleanup(func() {
		_, _ = runtime.DB.Exec(context.Background(), `DELETE FROM discovery_runs WHERE query=$1`, "integration-fresh-tv-"+suffix)
		for _, entityID := range entityIDs {
			cleanupDiscoveryTVEntity(runtime, entityID)
		}
		cleanupDiscoveryFixtureObservations(runtime, fixtures)
	})

	routes := []struct {
		fixture int
		scheme  string
		value   string
	}{
		{fixture: 0, scheme: "tmdb", value: strconv.FormatInt(fixtures[0].TMDB, 10)},
		{fixture: 1, scheme: "imdb", value: fixtures[1].IMDb},
		{fixture: 2, scheme: "tvdb", value: strconv.FormatInt(fixtures[2].TVDB, 10)},
	}
	for _, route := range routes {
		request := Request{Kind: KindTVShow, Identifiers: []Identifier{{Scheme: route.scheme, Value: route.value}}}
		result, handled, err := service.ResolveFreshIdentifiers(ctx, request, 0, providercredentials.Credentials{})
		if err != nil || !handled || result.EntityID == "" || result.Status != "completed" || len(result.Candidates) != 0 {
			t.Fatalf("fresh %s route result=%+v handled=%v err=%v", route.scheme, result, handled, err)
		}
		var kind string
		if err := runtime.DB.QueryRow(ctx, `SELECT kind FROM entities WHERE id=$1`, result.EntityID).Scan(&kind); err != nil || kind != KindTVShow {
			t.Fatalf("fresh %s canonical entity=%s kind=%q err=%v", route.scheme, result.EntityID, kind, err)
		}
		entityIDs = append(entityIDs, result.EntityID)
	}
	titleResult, err := service.DiscoverTV(ctx, Request{Kind: KindTVShow, Query: fixtures[0].Title, Hints: Hints{Year: 2020}}, 0, "")
	if err != nil || len(titleResult.Candidates) == 0 || titleResult.Candidates[0].Resolution.Provider != "tmdb" || titleResult.Candidates[0].ExistingEntityID != entityIDs[0] {
		t.Fatalf("TMDB-first TV title discovery result=%+v err=%v", titleResult, err)
	}
	animeResult, err := service.DiscoverAnime(ctx, Request{Kind: KindAnime, Query: fixtures[0].Title, Hints: Hints{Year: 2020}}, 0, "")
	if err != nil || len(animeResult.Candidates) == 0 || animeResult.Candidates[0].Resolution.Provider != "tmdb" {
		t.Fatalf("TMDB-first anime title discovery result=%+v err=%v", animeResult, err)
	}
	freshAnime, handled, err := service.ResolveFreshIdentifiers(ctx, Request{Kind: KindAnime, Identifiers: []Identifier{{Scheme: "tmdb", Value: strconv.FormatInt(fixtures[0].TMDB, 10)}}}, 0, providercredentials.Credentials{})
	if err != nil || !handled || freshAnime.EntityID == "" || freshAnime.EntityID == entityIDs[0] {
		t.Fatalf("TMDB-first anime identity result=%+v handled=%v err=%v", freshAnime, handled, err)
	}
	var animeKind string
	if err := runtime.DB.QueryRow(ctx, `SELECT kind FROM entities WHERE id=$1`, freshAnime.EntityID).Scan(&animeKind); err != nil || animeKind != KindAnime {
		t.Fatalf("anime entity=%s kind=%q err=%v", freshAnime.EntityID, animeKind, err)
	}
	entityIDs = append(entityIDs, freshAnime.EntityID)

	knownTVDB := Identifier{Scheme: "tvdb", Value: strconv.FormatInt(fixtures[0].TVDB, 10)}
	freshTMDB := Identifier{Scheme: "tmdb", Value: strconv.FormatInt(fixtures[0].TMDB, 10)}
	// Simulate mixed evidence arriving before the newer provider claim has been
	// persisted. The durable worker must crosswalk and reattach it to the known
	// UUID rather than completing prematurely or allocating a duplicate.
	if _, err := runtime.DB.Exec(ctx, `DELETE FROM external_id_claims WHERE entity_id=$1 AND entity_kind='tv_show' AND provider='tmdb' AND namespace='tv' AND normalized_value=$2`, entityIDs[0], freshTMDB.Value); err != nil {
		t.Fatal(err)
	}
	agreement := Request{Kind: KindTVShow, Identifiers: []Identifier{knownTVDB, freshTMDB}}
	local, handled, err := service.ResolveKnownIdentifiers(ctx, agreement)
	if err != nil || handled || local.EntityID != entityIDs[0] {
		t.Fatalf("mixed local agreement prematurely completed: result=%+v handled=%v err=%v", local, handled, err)
	}
	result, handled, err := service.ResolveFreshIdentifiers(ctx, agreement, 0, providercredentials.Credentials{})
	if err != nil || !handled || result.EntityID != entityIDs[0] || result.Recommendation != "corroborated_identity" {
		t.Fatalf("mixed agreement result=%+v handled=%v err=%v", result, handled, err)
	}
	if outcomeForScheme(result.IdentifierEvidence, "tmdb") != "corroborating" {
		t.Fatalf("mixed agreement evidence=%+v", result.IdentifierEvidence)
	}

	conflict := Request{
		Kind:  KindTVShow,
		Query: "integration-fresh-tv-" + suffix,
		Identifiers: []Identifier{
			knownTVDB,
			{Scheme: "tmdb", Value: strconv.FormatInt(fixtures[3].TMDB, 10)},
		},
	}
	run, err := EnsureRun(ctx, runtime, conflict)
	if err != nil {
		t.Fatal(err)
	}
	result, handled, err = service.ResolveFreshIdentifiers(ctx, conflict, 0, providercredentials.Credentials{})
	if err != nil || !handled || result.EntityID != "" || result.Status != "needs_selection" || len(result.Candidates) != 2 {
		t.Fatalf("mixed conflict result=%+v handled=%v err=%v", result, handled, err)
	}
	for _, candidate := range result.Candidates {
		if candidate.Resolution.Provider != "heya" || candidate.Resolution.Namespace != "entity" {
			t.Fatalf("mixed conflict retained a provider root: %+v", candidate.Resolution)
		}
		if candidate.Resolution.Value != entityIDs[0] {
			entityIDs = append(entityIDs, candidate.Resolution.Value)
		}
	}
	if err := Complete(ctx, runtime, run.RequestHash, result); err != nil {
		t.Fatal(err)
	}
	completed, err := GetRun(ctx, runtime, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range completed.Result.Candidates {
		if candidate.CandidateRef == "" {
			t.Fatal("mixed conflict candidate has no opaque reference")
		}
		body, _ := json.Marshal(candidate)
		for _, forbidden := range []string{"provider", "namespace", "resolution", "identity"} {
			if strings.Contains(string(body), `"`+forbidden+`"`) {
				t.Fatalf("mixed conflict candidate leaked %s: %s", forbidden, body)
			}
		}
	}
}

func TestIntegrationFreshArtistProviderRootsMustConverge(t *testing.T) {
	if os.Getenv("HEYA_METADATA_INTEGRATION") != "1" {
		t.Skip("set HEYA_METADATA_INTEGRATION=1 to use the local platform stack")
	}
	ctx := context.Background()
	suffix := fmt.Sprint(time.Now().UnixNano())
	mbid := "60000000-0000-4000-8000-" + suffix[len(suffix)-12:]
	conflictingMBID := "61000000-0000-4000-8000-" + suffix[len(suffix)-12:]
	conflictingReleaseID := "71000000-0000-4000-8000-" + suffix[len(suffix)-12:]
	collaborativeReleaseID := "72000000-0000-4000-8000-" + suffix[len(suffix)-12:]
	appleID := suffix
	appleNumeric, err := strconv.ParseInt(appleID, 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	artistName := "Provider Convergence " + suffix
	providerServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		switch {
		case request.URL.Path == "/artist/"+mbid:
			_ = json.NewEncoder(response).Encode(map[string]any{
				"id": mbid, "name": artistName, "sort-name": artistName, "type": "Person", "country": "US",
				"relations": []any{map[string]any{"target-type": "url", "type": "streaming", "url": map[string]any{"resource": "https://music.apple.com/us/artist/provider-convergence/" + appleID}}},
			})
		case request.URL.Path == "/artist/"+conflictingMBID:
			_ = json.NewEncoder(response).Encode(map[string]any{
				"id": conflictingMBID, "name": "Different Artist " + suffix, "sort-name": "Different Artist " + suffix, "type": "Person", "country": "US", "relations": []any{},
			})
		case request.URL.Path == "/release-group/"+conflictingReleaseID:
			_ = json.NewEncoder(response).Encode(map[string]any{
				"id": conflictingReleaseID, "title": "Contradictory Release", "first-release-date": "2024-01-01", "primary-type": "Album",
				"artist-credit": []any{map[string]any{"artist": map[string]any{"id": conflictingMBID, "name": "Different Artist " + suffix}}},
			})
		case request.URL.Path == "/release-group/"+collaborativeReleaseID:
			_ = json.NewEncoder(response).Encode(map[string]any{
				"id": collaborativeReleaseID, "title": "Shared Release", "first-release-date": "2023-01-01", "primary-type": "Single",
				"artist-credit": []any{
					map[string]any{"name": artistName, "joinphrase": " x ", "artist": map[string]any{"id": mbid, "name": artistName, "aliases": []any{map[string]any{"name": "Localized " + artistName}}}},
					map[string]any{"name": "Different Artist " + suffix, "artist": map[string]any{"id": conflictingMBID, "name": "Different Artist " + suffix}},
				},
			})
		case request.URL.Path == "/lookup" && request.URL.Query().Get("id") == appleID:
			_ = json.NewEncoder(response).Encode(map[string]any{
				"resultCount": 3,
				"results": []any{
					map[string]any{"wrapperType": "artist", "artistType": "Artist", "artistName": artistName, "artistId": appleNumeric, "artistLinkUrl": "https://music.apple.com/us/artist/provider-convergence/" + appleID, "primaryGenreName": "Pop", "primaryGenreId": 14},
					map[string]any{"wrapperType": "collection", "collectionType": "Album", "collectionId": 700000001, "collectionName": "Convergence One", "artistId": appleNumeric, "artistName": artistName, "releaseDate": "2020-01-01T00:00:00Z", "trackCount": 10},
					map[string]any{"wrapperType": "collection", "collectionType": "Album", "collectionId": 700000002, "collectionName": "Convergence Two", "artistId": appleNumeric, "artistName": artistName, "releaseDate": "2021-01-01T00:00:00Z", "trackCount": 11},
				},
			})
		default:
			response.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(response).Encode(map[string]any{"error": "fixture route not found", "path": request.URL.Path})
		}
	}))
	t.Cleanup(providerServer.Close)
	blobServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.Method {
		case http.MethodPut:
			response.WriteHeader(http.StatusOK)
		case http.MethodDelete:
			response.WriteHeader(http.StatusNoContent)
		default:
			response.WriteHeader(http.StatusNotFound)
			_, _ = response.Write([]byte(`<Error><Code>NoSuchKey</Code></Error>`))
		}
	}))
	t.Cleanup(blobServer.Close)

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg.S3.Endpoint = blobServer.URL
	cfg.S3.Bucket = "fixture"
	cfg.S3.Prefix = "artist-convergence"
	cfg.S3.AccessKeyID = "fixture-access"
	cfg.S3.SecretAccessKey = "fixture-secret"
	cfg.Providers.MusicBrainz.BaseURL = providerServer.URL
	cfg.Providers.MusicBrainz.RequestsPerSecond = 1000
	cfg.Providers.MusicBrainz.UserAgent = "HeyaMetadata/integration"
	cfg.Providers.Apple.BaseURL = providerServer.URL
	cfg.Providers.Apple.RequestsPerSecond = 1000
	runtime, err := platform.Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(runtime.Close)

	result, handled, err := NewService(runtime).ResolveFreshIdentifiers(ctx, Request{
		Kind: KindArtist, Query: artistName,
		Identifiers: []Identifier{{Scheme: "apple", Value: appleID}, {Scheme: "musicbrainz", Value: mbid}},
	}, 0, providercredentials.Credentials{})
	if err != nil || !handled || result.EntityID == "" || result.Status != "completed" || result.Recommendation != "corroborated_identity" || len(result.Candidates) != 0 {
		t.Fatalf("result=%+v handled=%v err=%v", result, handled, err)
	}
	t.Cleanup(func() { cleanupDiscoveryArtistEntity(runtime, result.EntityID, mbid, appleID) })
	collaborative, handled, err := NewService(runtime).ResolveFreshIdentifiers(ctx, Request{
		Kind: KindArtist, Query: "Localized " + artistName,
		Identifiers: []Identifier{{Scheme: "musicbrainz", Value: mbid}},
		Hints: Hints{Aliases: []string{artistName}, Releases: []ReleaseHint{{
			Title: "Shared Release", Year: 2023, Type: "single",
			Identifiers: []Identifier{{Scheme: "musicbrainz", Value: collaborativeReleaseID}},
		}}},
	}, 0, providercredentials.Credentials{})
	if err != nil || !handled || collaborative.EntityID != result.EntityID || collaborative.Status != "completed" || collaborative.Recommendation != "corroborated_identity" || len(collaborative.Candidates) != 0 {
		t.Fatalf("collaborative result=%+v handled=%v err=%v", collaborative, handled, err)
	}
	known, handled, err := NewService(runtime).ResolveKnownIdentifiers(ctx, Request{
		Kind: KindArtist, Query: artistName,
		Identifiers: []Identifier{{Scheme: "apple", Value: appleID}},
		Hints:       Hints{Releases: []ReleaseHint{{Title: "Contradictory release", Year: 2024, Identifiers: []Identifier{{Scheme: "musicbrainz", Value: "70000000-0000-4000-8000-000000000001"}}}}},
	})
	if err != nil || handled || known.EntityID != result.EntityID {
		t.Fatalf("known release evidence shortcut result=%+v handled=%v err=%v", known, handled, err)
	}
	conflict, handled, err := NewService(runtime).ResolveFreshIdentifiers(ctx, Request{
		Kind: KindArtist, Query: artistName,
		Identifiers: []Identifier{{Scheme: "apple", Value: appleID}},
		Hints:       Hints{Releases: []ReleaseHint{{Title: "Contradictory Release", Year: 2024, Type: "album", Identifiers: []Identifier{{Scheme: "musicbrainz", Value: conflictingReleaseID}}}}},
	}, 0, providercredentials.Credentials{})
	if err != nil || !handled || conflict.EntityID != "" || conflict.Status != "needs_selection" || conflict.Recommendation != "conflicting_identifiers" || len(conflict.Candidates) != 2 {
		t.Fatalf("release conflict result=%+v handled=%v err=%v", conflict, handled, err)
	}
	var conflictingEntityID string
	if err := runtime.DB.QueryRow(ctx, `SELECT entity_id::text FROM external_id_claims WHERE entity_kind='artist' AND provider='musicbrainz' AND namespace='artist' AND normalized_value=$1 AND state='accepted'`, conflictingMBID).Scan(&conflictingEntityID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanupDiscoveryArtistEntity(runtime, conflictingEntityID, conflictingMBID, "") })
	for _, scheme := range []string{"apple", "musicbrainz"} {
		if outcomeForScheme(result.IdentifierEvidence, scheme) != map[string]string{"apple": "corroborating", "musicbrainz": "resolved"}[scheme] {
			t.Fatalf("identifier evidence=%+v", result.IdentifierEvidence)
		}
	}
	var claims int
	if err := runtime.DB.QueryRow(ctx, `SELECT count(*) FROM external_id_claims WHERE entity_id=$1 AND entity_kind='artist' AND state='accepted' AND ((provider='musicbrainz' AND normalized_value=$2) OR (provider='apple' AND normalized_value=$3))`, result.EntityID, mbid, appleID).Scan(&claims); err != nil || claims != 2 {
		t.Fatalf("accepted claims=%d err=%v", claims, err)
	}
}

func cleanupDiscoveryArtistEntity(runtime *platform.Runtime, entityID, mbid, appleID string) {
	ctx := context.Background()
	for _, statement := range []string{
		`DELETE FROM change_log WHERE entity_id=$1`,
		`DELETE FROM change_outbox WHERE entity_id=$1`,
		`DELETE FROM provider_refresh_states WHERE entity_id=$1`,
		`DELETE FROM artist_top_tracks WHERE artist_entity_id=$1`,
		`DELETE FROM artist_top_track_snapshots WHERE artist_entity_id=$1`,
		`DELETE FROM artist_catalog_promotions WHERE artist_entity_id=$1`,
		`DELETE FROM entity_relations WHERE source_entity_id=$1 OR target_entity_id=$1`,
		`DELETE FROM api_document_provenance WHERE entity_id=$1`,
		`DELETE FROM api_documents WHERE entity_id=$1`,
		`DELETE FROM search_names WHERE entity_id=$1`,
		`DELETE FROM search_entities WHERE entity_id=$1`,
		`DELETE FROM image_candidates WHERE entity_id=$1`,
		`DELETE FROM canonical_artists WHERE entity_id=$1`,
		`DELETE FROM normalized_records WHERE entity_id=$1`,
		`DELETE FROM external_id_claims WHERE entity_id=$1`,
		`DELETE FROM entity_access_stats WHERE entity_id=$1`,
		`DELETE FROM entity_slugs WHERE entity_id=$1`,
		`DELETE FROM entities WHERE id=$1`,
	} {
		_, _ = runtime.DB.Exec(ctx, statement, entityID)
	}
	_, _ = runtime.DB.Exec(ctx, `DELETE FROM provider_observations WHERE (provider='musicbrainz' AND provider_record_id=$1) OR (provider='apple' AND provider_record_id=$2)`, mbid, appleID)
}

func tvIdentifierFixtureHandler(fixtures []tvIdentifierFixture) http.Handler {
	byTMDB := map[string]tvIdentifierFixture{}
	byIMDb := map[string]tvIdentifierFixture{}
	byTVDB := map[string]tvIdentifierFixture{}
	byTVMaze := map[string]tvIdentifierFixture{}
	for _, fixture := range fixtures {
		byTMDB[strconv.FormatInt(fixture.TMDB, 10)] = fixture
		byIMDb[fixture.IMDb] = fixture
		byTVDB[strconv.FormatInt(fixture.TVDB, 10)] = fixture
		byTVMaze[strconv.FormatInt(fixture.TVMaze, 10)] = fixture
	}
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		path := strings.Trim(request.URL.Path, "/")
		if path == "search/tv" {
			for _, fixture := range fixtures {
				if fixture.Title == request.URL.Query().Get("query") {
					_ = json.NewEncoder(response).Encode(map[string]any{"results": []any{map[string]any{"id": fixture.TMDB, "name": fixture.Title, "original_name": fixture.Title, "original_language": "en", "first_air_date": "2020-01-01", "origin_country": []string{"US"}, "genre_ids": []int{16, 18}, "popularity": 100}}})
					return
				}
			}
			_ = json.NewEncoder(response).Encode(map[string]any{"results": []any{}})
			return
		}
		if strings.HasPrefix(path, "tv/") && strings.HasSuffix(path, "/external_ids") {
			id := strings.TrimSuffix(strings.TrimPrefix(path, "tv/"), "/external_ids")
			if fixture, ok := byTMDB[id]; ok {
				_ = json.NewEncoder(response).Encode(map[string]any{"id": fixture.TMDB, "imdb_id": fixture.IMDb, "tvdb_id": fixture.TVDB})
				return
			}
		}
		if strings.HasPrefix(path, "tv/") {
			if fixture, ok := byTMDB[strings.TrimPrefix(path, "tv/")]; ok {
				_ = json.NewEncoder(response).Encode(map[string]any{
					"id": fixture.TMDB, "name": fixture.Title, "original_name": fixture.Title,
					"original_language": "en", "first_air_date": "2020-01-01", "status": "Ended", "type": "Scripted",
					"origin_country": []string{"US"}, "genres": []any{map[string]any{"id": 16, "name": "Animation"}}, "number_of_seasons": 0, "number_of_episodes": 0, "seasons": []any{},
					"external_ids": map[string]any{"tvdb_id": fixture.TVDB, "imdb_id": fixture.IMDb},
				})
				return
			}
		}
		if path == "lookup/shows" {
			fixture, ok := byIMDb[request.URL.Query().Get("imdb")]
			if !ok {
				fixture, ok = byTVDB[request.URL.Query().Get("thetvdb")]
			}
			if ok {
				_ = json.NewEncoder(response).Encode(map[string]any{"id": fixture.TVMaze, "name": fixture.Title})
				return
			}
		}
		if strings.HasPrefix(path, "shows/") {
			if fixture, ok := byTVMaze[strings.TrimPrefix(path, "shows/")]; ok {
				_ = json.NewEncoder(response).Encode(map[string]any{
					"id": fixture.TVMaze, "name": fixture.Title, "language": "English", "type": "Scripted",
					"status": "Ended", "premiered": "2020-01-01",
					"externals": map[string]any{"thetvdb": fixture.TVDB, "imdb": fixture.IMDb},
					"_embedded": map[string]any{"akas": []any{}, "seasons": []any{}, "episodes": []any{}, "images": []any{}, "cast": []any{}, "crew": []any{}},
				})
				return
			}
		}
		if strings.HasPrefix(path, "find/") {
			value := strings.TrimPrefix(path, "find/")
			fixture, ok := byIMDb[value]
			if !ok {
				fixture, ok = byTVDB[value]
			}
			if ok {
				_ = json.NewEncoder(response).Encode(map[string]any{"tv_results": []any{map[string]any{"id": fixture.TMDB}}})
				return
			}
			response.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(response).Encode(map[string]any{"success": false, "fixture": path})
			return
		}
		response.WriteHeader(http.StatusNotFound)
		_, _ = response.Write([]byte(`{"error":"fixture route not found"}`))
	})
}

func outcomeForScheme(evidence []IdentifierEvidence, scheme string) string {
	for _, item := range evidence {
		if item.Scheme == scheme {
			return item.Outcome
		}
	}
	return ""
}

func cleanupDiscoveryTVEntity(runtime *platform.Runtime, entityID string) {
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
}

func cleanupDiscoveryFixtureObservations(runtime *platform.Runtime, fixtures []tvIdentifierFixture) {
	ctx := context.Background()
	values := []string{}
	for _, fixture := range fixtures {
		values = append(values,
			strconv.FormatInt(fixture.TMDB, 10),
			strconv.FormatInt(fixture.TVDB, 10),
			strconv.FormatInt(fixture.TVMaze, 10),
			fixture.IMDb,
		)
	}
	rows, err := runtime.DB.Query(ctx, `SELECT DISTINCT blob_checksum FROM provider_observations WHERE provider_record_id=ANY($1::text[]) AND blob_checksum IS NOT NULL`, values)
	if err != nil {
		return
	}
	checksums := []string{}
	for rows.Next() {
		var checksum string
		if rows.Scan(&checksum) == nil {
			checksums = append(checksums, checksum)
		}
	}
	rows.Close()
	_, _ = runtime.DB.Exec(ctx, `DELETE FROM provider_observations WHERE provider_record_id=ANY($1::text[])`, values)
	for _, checksum := range checksums {
		_, _ = runtime.DB.Exec(ctx, `DELETE FROM source_blobs WHERE checksum=$1 AND NOT EXISTS (SELECT 1 FROM provider_observations WHERE blob_checksum=$1)`, checksum)
	}
}

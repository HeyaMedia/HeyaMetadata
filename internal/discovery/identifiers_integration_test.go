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

	knownTVDB := Identifier{Scheme: "tvdb", Value: strconv.FormatInt(fixtures[0].TVDB, 10)}
	freshTMDB := Identifier{Scheme: "tmdb", Value: strconv.FormatInt(fixtures[0].TMDB, 10)}
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
		if strings.HasPrefix(path, "tv/") && strings.HasSuffix(path, "/external_ids") {
			id := strings.TrimSuffix(strings.TrimPrefix(path, "tv/"), "/external_ids")
			if fixture, ok := byTMDB[id]; ok {
				_ = json.NewEncoder(response).Encode(map[string]any{"id": fixture.TMDB, "imdb_id": fixture.IMDb, "tvdb_id": fixture.TVDB})
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

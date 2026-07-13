package tmdb

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/HeyaMedia/HeyaMetadata/internal/config"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers"
)

func TestCollectorUsesRequestScopedAPIKeyWithoutPuttingItInCacheIdentity(t *testing.T) {
	t.Parallel()
	const secret = "user-tmdb-key"
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Query().Get("api_key") != secret {
			t.Errorf("request-scoped API key was not forwarded")
		}
		if request.Header.Get("Authorization") != "" {
			t.Errorf("server token should not be sent with a user API key")
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"id":603,"title":"The Matrix"}`))
	}))
	defer server.Close()

	client := NewCached(config.TMDBConfig{BaseURL: server.URL, Language: "en-US", Token: "server-token"}, nil, secret)
	payloads, err := client.Collect(context.Background(), providers.Identifier{Provider: "tmdb", Namespace: "movie", Value: "603"})
	if err != nil {
		t.Fatal(err)
	}
	if len(payloads) != 1 || strings.Contains(payloads[0].RequestKey, secret) {
		t.Fatalf("unsafe or unexpected payload: %+v", payloads)
	}
}

func TestMovieSearchUsesStructuredParametersAndSafeCacheIdentity(t *testing.T) {
	t.Parallel()
	const secret = "search-key"
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/search/movie" || request.URL.Query().Get("query") != "The Matrix" || request.URL.Query().Get("year") != "1999" || request.URL.Query().Get("page") != "2" {
			t.Errorf("unexpected search request: %s", request.URL.String())
		}
		if request.URL.Query().Get("api_key") != secret {
			t.Error("request-scoped key was not forwarded")
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"results":[{"id":603,"title":"The Matrix"}]}`))
	}))
	defer server.Close()

	client := NewCached(config.TMDBConfig{BaseURL: server.URL, Language: "en-US"}, nil, secret)
	payload, err := client.SearchMovies(context.Background(), " The Matrix ", 1999, 2)
	if err != nil {
		t.Fatal(err)
	}
	if payload.StatusCode != http.StatusOK || strings.Contains(payload.RequestKey, secret) || !strings.Contains(payload.RequestKey, "year=1999") {
		t.Fatalf("unsafe or unexpected search payload: %+v", payload)
	}
}

func TestCollectPersonRequestsBoundedEnrichmentScopes(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/person/6384" {
			t.Errorf("unexpected path %s", request.URL.Path)
		}
		if got := request.URL.Query().Get("append_to_response"); got != appendedPersonScopes {
			t.Errorf("append_to_response = %q", got)
		}
		if got := request.URL.Query().Get("include_image_language"); got != "en,en,null" {
			t.Errorf("include_image_language = %q", got)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"id":6384,"name":"Keanu Reeves"}`))
	}))
	defer server.Close()
	client := NewCached(config.TMDBConfig{BaseURL: server.URL, Language: "en-US"}, nil, "key")
	payloads, err := client.CollectPerson(context.Background(), providers.Identifier{Provider: "tmdb", Namespace: "person", Value: "6384"})
	if err != nil {
		t.Fatal(err)
	}
	if len(payloads) != 1 || payloads[0].ProviderNamespace != "person" || strings.Contains(payloads[0].RequestKey, "key") {
		t.Fatalf("unexpected payload: %+v", payloads)
	}
}

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

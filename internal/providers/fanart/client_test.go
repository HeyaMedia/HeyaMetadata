package fanart

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/config"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers"
)

func TestCollectorForwardsKeysWithoutPuttingThemInRequestIdentity(t *testing.T) {
	t.Parallel()
	const projectKey = "project-secret"
	const clientKey = "personal-secret"
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/movies/603" || request.URL.Query().Get("api_key") != projectKey || request.URL.Query().Get("client_key") != clientKey {
			t.Errorf("unexpected Fanart.tv request: %s", request.URL.String())
		}
		_, _ = writer.Write([]byte(`{"name":"The Matrix","tmdb_id":"603","imdb_id":"tt0133093"}`))
	}))
	defer server.Close()
	client := NewCached(config.FanartConfig{BaseURL: server.URL, APIKey: projectKey}, nil, clientKey)
	payloads, err := client.Collect(context.Background(), providers.Identifier{Provider: "tmdb", Namespace: "movie", Value: "603"})
	if err != nil {
		t.Fatal(err)
	}
	if len(payloads) != 1 || strings.Contains(payloads[0].RequestKey, projectKey) || strings.Contains(payloads[0].RequestKey, clientKey) {
		t.Fatalf("unsafe payload identity: %+v", payloads)
	}
}

func TestEmptySuccessGetsShortNegativeReuse(t *testing.T) {
	t.Parallel()
	payload := providers.Payload{StatusCode: http.StatusOK, Body: []byte(`{}`)}
	classifyReuse(&payload)
	if payload.ReuseDurationOverride == nil || *payload.ReuseDurationOverride != time.Hour {
		t.Fatalf("classification: %+v", payload.ReuseDurationOverride)
	}
}

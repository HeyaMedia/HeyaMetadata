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
	classifyReuse("movie")(&payload)
	if payload.ReuseDurationOverride == nil || *payload.ReuseDurationOverride != time.Hour {
		t.Fatalf("classification: %+v", payload.ReuseDurationOverride)
	}
}

func TestCollectorSupportsTVDBSeries(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/tv/121361" {
			t.Errorf("unexpected Fanart.tv request path: %s", request.URL.Path)
		}
		_, _ = writer.Write([]byte(`{"name":"Game of Thrones","thetvdb_id":"121361","tvposter":[]}`))
	}))
	defer server.Close()
	client := New(config.FanartConfig{BaseURL: server.URL, APIKey: "project-key"})
	payloads, err := client.Collect(context.Background(), providers.Identifier{Provider: "tvdb", Namespace: "series", Value: "121361"})
	if err != nil {
		t.Fatal(err)
	}
	if len(payloads) != 1 || payloads[0].ProviderNamespace != "series" || payloads[0].RequestKey != "tv/121361" {
		t.Fatalf("payloads: %+v", payloads)
	}
}

func TestCollectorSupportsMusicBrainzArtist(t *testing.T) {
	t.Parallel()
	const mbid = "e134b52f-2e9e-4734-9bc3-bea9648d1fa1"
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/music/"+mbid {
			t.Errorf("unexpected Fanart.tv request path: %s", request.URL.Path)
		}
		_, _ = writer.Write([]byte(`{"name":"Ado","mbid_id":"` + mbid + `","hdmusiclogo":[]}`))
	}))
	defer server.Close()
	client := New(config.FanartConfig{BaseURL: server.URL, APIKey: "project-key"})
	payloads, err := client.Collect(context.Background(), providers.Identifier{Provider: "musicbrainz", Namespace: "artist", Value: mbid})
	if err != nil {
		t.Fatal(err)
	}
	if len(payloads) != 1 || payloads[0].ProviderNamespace != "artist" || payloads[0].RequestKey != "music/"+mbid {
		t.Fatalf("payloads: %+v", payloads)
	}
}

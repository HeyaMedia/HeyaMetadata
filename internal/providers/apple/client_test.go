package apple

import (
	"context"
	"github.com/HeyaMedia/HeyaMetadata/internal/config"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLookupAndSearchPreserveStorefrontInIdentity(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"resultCount":1,"results":[{}]}`))
	}))
	defer server.Close()
	client := New(config.AppleConfig{BaseURL: server.URL, MusicBaseURL: server.URL, Country: "JP", RequestsPerSecond: 1000})
	lookup, err := client.Collect(context.Background(), providers.Identifier{Provider: "apple", Namespace: "artist", Value: "136975"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(lookup[0].RequestKey, "country=JP") || !strings.Contains(lookup[0].RequestKey, "entity=album") {
		t.Fatalf("lookup key: %s", lookup[0].RequestKey)
	}
	search, err := client.Search(context.Background(), "track", "Björk", "IS", 10)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(search.RequestKey, "country=IS") || !strings.Contains(search.RequestKey, "entity=song") {
		t.Fatalf("search key: %s", search.RequestKey)
	}
}

func TestAppleMusicUsesBearerTokenAndDistinctIdentity(t *testing.T) {
	t.Parallel()
	const token = "signed-developer-token"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/catalog/us/albums/1616728060" || r.Header.Get("Authorization") != "Bearer "+token {
			t.Errorf("unexpected request: %s", r.URL.String())
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"1616728060","type":"albums"}]}`))
	}))
	defer server.Close()
	client := NewCached(config.AppleConfig{BaseURL: server.URL, MusicBaseURL: server.URL, Country: "US", RequestsPerSecond: 1000}, nil, token)
	payloads, err := client.Collect(context.Background(), providers.Identifier{Provider: "apple", Namespace: "album", Value: "1616728060"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(payloads[0].RequestKey, "music/") || strings.Contains(payloads[0].RequestKey, token) {
		t.Fatalf("request identity: %s", payloads[0].RequestKey)
	}
}

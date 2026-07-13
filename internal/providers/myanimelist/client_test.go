package myanimelist

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/HeyaMedia/HeyaMetadata/internal/config"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers"
)

func TestCollectUsesRequestScopedClientID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-MAL-CLIENT-ID") != "user-client" {
			t.Errorf("client ID=%q", r.Header.Get("X-MAL-CLIENT-ID"))
		}
		if r.URL.Path != "/manga/11" {
			t.Errorf("path=%s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"id":11,"title":"Naruto"}`))
	}))
	defer server.Close()
	c := New(config.MyAnimeListConfig{BaseURL: server.URL, RequestsPerSecond: 1000}, "user-client")
	values, err := c.Collect(context.Background(), providers.Identifier{Provider: "myanimelist", Namespace: "manga", Value: "11"})
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 1 {
		t.Fatalf("payloads=%d", len(values))
	}
}
func TestCollectRequiresClientID(t *testing.T) {
	c := New(config.MyAnimeListConfig{BaseURL: "https://example.invalid", RequestsPerSecond: 1}, "")
	_, err := c.Collect(context.Background(), providers.Identifier{Provider: "myanimelist", Namespace: "manga", Value: "11"})
	if err == nil {
		t.Fatal("expected missing client ID error")
	}
}

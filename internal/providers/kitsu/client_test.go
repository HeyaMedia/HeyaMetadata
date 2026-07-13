package kitsu

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/HeyaMedia/HeyaMetadata/internal/config"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers"
)

func TestSearchAndCollect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "application/vnd.api+json" {
			t.Errorf("accept=%q", r.Header.Get("Accept"))
		}
		switch r.URL.Path {
		case "/manga":
			if r.URL.Query().Get("filter[text]") != "Naruto" {
				t.Errorf("query=%s", r.URL.RawQuery)
			}
		case "/manga/35", "/manga/35/mappings":
		default:
			t.Errorf("path=%s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/vnd.api+json")
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer server.Close()
	c := New(config.KitsuConfig{BaseURL: server.URL, RequestsPerSecond: 1000})
	if _, err := c.Search(context.Background(), "Naruto", 5); err != nil {
		t.Fatal(err)
	}
	values, err := c.Collect(context.Background(), providers.Identifier{Provider: "kitsu", Namespace: "manga", Value: "35"})
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 2 {
		t.Fatalf("payloads=%d", len(values))
	}
}

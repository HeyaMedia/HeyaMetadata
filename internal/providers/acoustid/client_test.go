package acoustid

import (
	"context"
	"github.com/HeyaMedia/HeyaMetadata/internal/config"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLookupUsesClientKeyAndDecodesRecording(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("client") != "secret" {
			t.Errorf("client key missing")
		}
		if r.URL.Query().Get("fingerprint") != "encoded" {
			t.Errorf("fingerprint missing")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","results":[{"id":"a","score":0.9,"recordings":[{"id":"mbid","title":"Song"}]}]}`))
	}))
	defer server.Close()
	client := New(config.AcoustIDConfig{BaseURL: server.URL, RequestsPerSecond: 100})
	result, err := client.Lookup(context.Background(), "encoded", 30, "secret")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Results) != 1 || result.Results[0].Recordings[0].Title != "Song" {
		t.Fatalf("result: %+v", result)
	}
}

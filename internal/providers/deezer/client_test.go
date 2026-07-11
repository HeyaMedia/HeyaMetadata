package deezer

import (
	"context"
	"github.com/HeyaMedia/HeyaMetadata/internal/config"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSearchAndArtistAlbumsAreExplicitlyPaged(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"data":[],"total":0}`)) }))
	defer server.Close()
	client := New(config.DeezerConfig{BaseURL: server.URL, RequestsPerSecond: 1000})
	search, err := client.Search(context.Background(), "track", "Human Behaviour", 12, 24)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(search.RequestKey, "limit=12") || !strings.Contains(search.RequestKey, "index=24") {
		t.Fatal(search.RequestKey)
	}
	albums, err := client.ArtistAlbums(context.Background(), "1087", 100, 200)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(albums.RequestKey, "index=200") {
		t.Fatal(albums.RequestKey)
	}
}
func TestLogicalErrorIsNotShared(t *testing.T) {
	t.Parallel()
	payload := providers.Payload{StatusCode: http.StatusOK, Body: []byte(`{"error":{"type":"Exception","message":"Quota exceeded","code":4}}`)}
	classify(12 * time.Hour)(&payload)
	if payload.ReuseDurationOverride == nil || *payload.ReuseDurationOverride != 0 {
		t.Fatalf("reuse: %+v", payload.ReuseDurationOverride)
	}
}

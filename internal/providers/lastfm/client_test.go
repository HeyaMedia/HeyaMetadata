package lastfm

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

func TestAPIKeyStaysOutOfRequestIdentity(t *testing.T) {
	t.Parallel()
	const secret = "lastfm-secret"
	const mbid = "b10bbbfc-cf9e-42e0-be17-e2c3e1d2600d"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("api_key") != secret || r.URL.Query().Get("method") != "artist.getInfo" {
			t.Error("unexpected request")
		}
		_, _ = w.Write([]byte(`{"artist":{"name":"The Beatles"}}`))
	}))
	defer server.Close()
	client := NewCached(config.LastFMConfig{BaseURL: server.URL, RequestsPerSecond: 1000}, nil, secret)
	payloads, err := client.Collect(context.Background(), providers.Identifier{Provider: "musicbrainz", Namespace: "artist", Value: mbid})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(payloads[0].RequestKey, secret) {
		t.Fatal("secret entered request identity")
	}
}
func TestLogicalNotFoundUsesShortNegativeReuse(t *testing.T) {
	t.Parallel()
	payload := providers.Payload{StatusCode: http.StatusOK, Body: []byte(`{"error":6,"message":"Artist not found"}`)}
	classify(12 * time.Hour)(&payload)
	if payload.ReuseDurationOverride == nil || *payload.ReuseDurationOverride != time.Hour {
		t.Fatalf("reuse: %+v", payload.ReuseDurationOverride)
	}
}

func TestAlbumLookupUsesReleaseNotReleaseGroupMBID(t *testing.T) {
	capability := New(config.LastFMConfig{}).Capability()
	accepted := map[string]bool{}
	for _, identifier := range capability.AcceptedIdentifiers {
		accepted[identifier.Namespace] = true
	}
	if !accepted["release"] || accepted["release_group"] {
		t.Fatalf("accepted identifiers: %+v", capability.AcceptedIdentifiers)
	}
}

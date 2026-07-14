package lrclib

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/config"
)

func TestRequestTimeoutAllowsSlowLRCLIBResponses(t *testing.T) {
	if requestTimeout < 15*time.Second {
		t.Fatalf("request timeout %s is too short for LRCLIB's documented endpoint", requestTimeout)
	}
}

func TestGetUsesDocumentedExactSignatureEndpointAndUserAgent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/get" || r.URL.Query().Get("track_name") != "Fuhen" || r.URL.Query().Get("duration") != "231" {
			t.Errorf("request: %s", r.URL.String())
		}
		if r.Header.Get("User-Agent") != "HeyaMetadata/test" {
			t.Errorf("user agent: %q", r.Header.Get("User-Agent"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":42,"trackName":"Fuhen","artistName":"ano","albumName":"Fuhen","duration":231,"instrumental":false,"plainLyrics":"words","syncedLyrics":null}`))
	}))
	defer server.Close()
	client := New(config.LRCLIBConfig{BaseURL: server.URL, UserAgent: "HeyaMetadata/test", RequestsPerSecond: 1000})
	payload, err := client.Get(context.Background(), Signature{TrackName: "Fuhen", ArtistName: "ano", AlbumName: "Fuhen", Duration: 231})
	if err != nil || payload.StatusCode != http.StatusOK {
		t.Fatalf("payload=%+v err=%v", payload, err)
	}
}

func TestNormalizeLyricsAndRejectDurationMismatch(t *testing.T) {
	body := []byte(`{"id":42,"trackName":"Fuhen","artistName":"ano","albumName":"Fuhen","duration":231,"instrumental":false,"plainLyrics":" words ","syncedLyrics":"[00:01.00] words"}`)
	evidence, err := Normalize(body, "observation", time.Unix(1, 0).UTC(), Signature{Duration: 230})
	if err != nil {
		t.Fatal(err)
	}
	if evidence.ProviderRecordID != "42" || evidence.PlainLyrics != "words" || evidence.ContentChecksum == "" {
		t.Fatalf("evidence: %+v", evidence)
	}
	if _, err := Normalize(body, "observation", time.Now(), Signature{Duration: 220}); err == nil {
		t.Fatal("expected duration mismatch")
	}
}

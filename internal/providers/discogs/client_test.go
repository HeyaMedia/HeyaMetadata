package discogs

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

func TestTokenStaysOutOfRequestIdentity(t *testing.T) {
	t.Parallel()
	const secret = "discogs-secret"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Discogs token="+secret {
			t.Error("missing token")
		}
		if r.Header.Get("User-Agent") != "HeyaMetadata/test" {
			t.Error("missing user agent")
		}
		_, _ = w.Write([]byte(`{"id":1,"name":"Artist"}`))
	}))
	defer server.Close()
	client := NewCached(config.DiscogsConfig{BaseURL: server.URL, RequestsPerSecond: 1000, UserAgent: "HeyaMetadata/test"}, nil, secret)
	payloads, err := client.Collect(context.Background(), providers.Identifier{Provider: "discogs", Namespace: "artist", Value: "1"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(payloads[0].RequestKey, secret) {
		t.Fatal("secret entered request identity")
	}
}
func TestSearchRequiresCredentialOnNetworkFetch(t *testing.T) {
	t.Parallel()
	client := New(config.DiscogsConfig{BaseURL: "https://api.discogs.com", RequestsPerSecond: 1, UserAgent: "HeyaMetadata/test"})
	if _, err := client.Search(context.Background(), "Björk", "artist", 10, 1); err == nil {
		t.Fatal("expected credential error")
	}
}
func TestBarcodeSearchKeepsCredentialOutOfIdentity(t *testing.T) {
	t.Parallel()
	const secret = "secret"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("barcode") != "123" || r.Header.Get("Authorization") != "Discogs token="+secret {
			t.Errorf("request: %s", r.URL.String())
		}
		_, _ = w.Write([]byte(`{"results":[]}`))
	}))
	defer server.Close()
	client := NewCached(config.DiscogsConfig{BaseURL: server.URL, RequestsPerSecond: 1000, UserAgent: "test"}, nil, secret)
	payload, err := client.SearchReleaseByBarcode(context.Background(), "123", 5)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(payload.RequestKey, secret) {
		t.Fatal("credential leaked")
	}
}

func TestDiscogsRateLimitCooldown(t *testing.T) {
	t.Parallel()
	if got := discogsCooldown(providers.Payload{StatusCode: http.StatusTooManyRequests, Headers: http.Header{"Retry-After": {"2"}}}); got != 2*time.Second {
		t.Fatalf("Retry-After cooldown: %s", got)
	}
	if got := discogsCooldown(providers.Payload{StatusCode: http.StatusOK, Headers: http.Header{"X-Discogs-Ratelimit-Remaining": {"0"}}}); got != time.Minute {
		t.Fatalf("budget cooldown: %s", got)
	}
	if got := discogsCooldown(providers.Payload{StatusCode: http.StatusTooManyRequests, FromCache: true}); got != 0 {
		t.Fatalf("cached cooldown: %s", got)
	}
}

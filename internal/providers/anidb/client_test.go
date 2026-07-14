package anidb

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/config"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers"
)

func TestBannedResponseDefersFollowingNetworkRequests(t *testing.T) {
	t.Parallel()
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests++
		_, _ = writer.Write([]byte(`<error>Banned</error>`))
	}))
	defer server.Close()
	client := New(config.AniDBConfig{BaseURL: server.URL, Client: "heyatest", ClientVersion: 1})
	payloads, err := client.Collect(context.Background(), providers.Identifier{Provider: "anidb", Namespace: "anime", Value: "1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(payloads) != 1 || payloads[0].StatusCode != http.StatusTooManyRequests {
		t.Fatalf("payloads: %+v", payloads)
	}
	_, err = client.Collect(context.Background(), providers.Identifier{Provider: "anidb", Namespace: "anime", Value: "2"})
	var statusError *providers.StatusError
	if !errors.As(err, &statusError) || statusError.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("second request error: %v", err)
	}
	if requests != 1 {
		t.Fatalf("network requests: got %d, want 1", requests)
	}
}

func TestCollectorSendsRegisteredClientButSharesContentIdentity(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Query().Get("client") != "heyatest" || request.URL.Query().Get("aid") != "1" {
			t.Errorf("unexpected AniDB request: %s", request.URL.String())
		}
		if request.UserAgent() != "heya-media/1.0 anidb-titles-sync" {
			t.Errorf("unexpected AniDB user agent: %q", request.UserAgent())
		}
		_, _ = writer.Write([]byte(`<anime id="1"><type>TV Series</type></anime>`))
	}))
	defer server.Close()
	client := New(config.AniDBConfig{BaseURL: server.URL, Client: "heyatest", ClientVersion: 1, UserAgent: "heya-media/1.0 anidb-titles-sync"})
	payloads, err := client.Collect(context.Background(), providers.Identifier{Provider: "anidb", Namespace: "anime", Value: "1"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(payloads[0].RequestKey, "heyatest") {
		t.Fatal("registered client identifier unnecessarily fragmented shared content identity")
	}
}

func TestOtherLogicalErrorIsNotReusable(t *testing.T) {
	t.Parallel()
	payload := providers.Payload{StatusCode: http.StatusOK, Body: []byte(`<error>Invalid request</error>`)}
	classify("1")(&payload)
	if payload.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d, want %d", payload.StatusCode, http.StatusOK)
	}
	if payload.ReuseDurationOverride == nil || *payload.ReuseDurationOverride != time.Duration(0) {
		t.Fatalf("reuse: %+v", payload.ReuseDurationOverride)
	}
}

func TestAnimeNotFoundEnvelopeIsClassifiedAsNotFound(t *testing.T) {
	t.Parallel()
	payload := providers.Payload{StatusCode: http.StatusOK, Body: []byte(`<error>Anime not found</error>`)}
	classify("14921")(&payload)
	if payload.StatusCode != http.StatusNotFound {
		t.Fatalf("status: got %d, want %d", payload.StatusCode, http.StatusNotFound)
	}
	if payload.ReuseDurationOverride == nil || *payload.ReuseDurationOverride != time.Hour {
		t.Fatalf("reuse: %+v", payload.ReuseDurationOverride)
	}
}

func TestBannedEnvelopeIsClassifiedAsRateLimited(t *testing.T) {
	t.Parallel()
	payload := providers.Payload{StatusCode: http.StatusOK, Body: []byte(`<error>Banned</error>`)}
	classify("1")(&payload)
	if payload.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("status: got %d, want %d", payload.StatusCode, http.StatusTooManyRequests)
	}
	if payload.ReuseDurationOverride == nil || *payload.ReuseDurationOverride != 0 {
		t.Fatalf("reuse: %+v", payload.ReuseDurationOverride)
	}
}

func TestTitleDumpHasStableDailyCacheIdentity(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.UserAgent() != "heya-media/1.0 anidb-titles-sync" {
			t.Errorf("unexpected AniDB title user agent: %q", request.UserAgent())
		}
		_, _ = writer.Write([]byte(`<animetitles><anime aid="1"><title type="main" xml:lang="x-jat">Cowboy Bebop</title></anime></animetitles>`))
	}))
	defer server.Close()
	client := New(config.AniDBConfig{TitlesURL: server.URL, UserAgent: "heya-media/1.0 anidb-titles-sync"})
	payload, err := client.Titles(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if payload.ProviderNamespace != "anime_title_dump" || payload.RequestKey != "anime-titles.xml.gz" || payload.ReuseDurationOverride == nil || *payload.ReuseDurationOverride != 24*time.Hour {
		t.Fatalf("title dump payload: %+v", payload)
	}
}

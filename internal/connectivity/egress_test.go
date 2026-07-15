package connectivity

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
)

func TestPublicIPCacheRefreshAndMatch(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Accept") != "text/plain" {
			t.Errorf("Accept = %q", request.Header.Get("Accept"))
		}
		_, _ = response.Write([]byte(" 8.8.8.8\n"))
	}))
	defer server.Close()

	cache := NewPublicIPCache(server.URL, server.Client())
	if err := cache.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !cache.Matches(netip.MustParseAddr("8.8.8.8")) {
		t.Fatal("successful refresh did not match the public address")
	}
	if cache.Matches(netip.MustParseAddr("1.1.1.1")) {
		t.Fatal("cache matched a different public address")
	}
}

func TestPublicIPCacheRejectsInvalidResponseAndKeepsLastSuccess(t *testing.T) {
	t.Parallel()
	responseBody := "1.1.1.1"
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte(responseBody))
	}))
	defer server.Close()

	cache := NewPublicIPCache(server.URL, server.Client())
	if err := cache.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	responseBody = "127.0.0.1"
	if err := cache.Refresh(context.Background()); err == nil {
		t.Fatal("expected private echo address to be rejected")
	}
	if !cache.Matches(netip.MustParseAddr("1.1.1.1")) {
		t.Fatal("failed refresh discarded the last successful address")
	}
}

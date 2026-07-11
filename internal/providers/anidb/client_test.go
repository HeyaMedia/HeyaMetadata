package anidb

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/config"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers"
)

func TestCollectorSendsRegisteredClientButSharesContentIdentity(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Query().Get("client") != "heyatest" || request.URL.Query().Get("aid") != "1" {
			t.Errorf("unexpected AniDB request: %s", request.URL.String())
		}
		_, _ = writer.Write([]byte(`<anime id="1"><type>TV Series</type></anime>`))
	}))
	defer server.Close()
	client := New(config.AniDBConfig{BaseURL: server.URL, Client: "heyatest", ClientVersion: 1})
	payloads, err := client.Collect(context.Background(), providers.Identifier{Provider: "anidb", Namespace: "anime", Value: "1"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(payloads[0].RequestKey, "heyatest") {
		t.Fatal("registered client identifier unnecessarily fragmented shared content identity")
	}
}

func TestLogicalErrorIsNotReusable(t *testing.T) {
	t.Parallel()
	payload := providers.Payload{StatusCode: http.StatusOK, Body: []byte(`<error>Banned</error>`)}
	classify("1")(&payload)
	if payload.ReuseDurationOverride == nil || *payload.ReuseDurationOverride != time.Duration(0) {
		t.Fatalf("reuse: %+v", payload.ReuseDurationOverride)
	}
}

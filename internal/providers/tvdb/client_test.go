package tvdb

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/HeyaMedia/HeyaMetadata/internal/config"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers"
)

func TestCollectorLogsInOnceAndFetchesRemoteMatchAndMovie(t *testing.T) {
	t.Parallel()
	var logins atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/login":
			logins.Add(1)
			var body map[string]string
			_ = json.NewDecoder(request.Body).Decode(&body)
			if body["apikey"] != "tvdb-key" {
				t.Errorf("login did not receive API key")
			}
			_, _ = writer.Write([]byte(`{"status":"success","data":{"token":"bearer-token"}}`))
		case "/search/remoteid/tt0133093":
			if request.Header.Get("Authorization") != "Bearer bearer-token" {
				t.Errorf("missing search bearer token")
			}
			_, _ = writer.Write([]byte(`{"status":"success","data":[{"movie":{"id":123,"name":"The Matrix"}}]}`))
		case "/movies/123/extended":
			if request.Header.Get("Authorization") != "Bearer bearer-token" {
				t.Errorf("missing detail bearer token")
			}
			_, _ = writer.Write([]byte(`{"status":"success","data":{"id":123,"name":"The Matrix"}}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	client := New(config.TVDBConfig{APIKey: "tvdb-key", BaseURL: server.URL})
	payloads, err := client.Collect(context.Background(), providers.Identifier{Provider: "imdb", Namespace: "title", Value: "tt0133093"})
	if err != nil {
		t.Fatal(err)
	}
	if len(payloads) != 2 || payloads[1].ProviderRecordID != "123" || logins.Load() != 1 {
		t.Fatalf("unexpected collection: payloads=%+v logins=%d", payloads, logins.Load())
	}
}

func TestEmptyRemoteSearchGetsShortReuseOverride(t *testing.T) {
	t.Parallel()
	payload := providers.Payload{StatusCode: http.StatusOK, Body: []byte(`{"status":"success","data":[]}`)}
	classifyRemoteSearch(&payload)
	if payload.ReuseDurationOverride == nil || *payload.ReuseDurationOverride <= 0 {
		t.Fatal("empty remote search was not negative cached")
	}
}

func TestPersonCollectorUsesExactTVDBPersonID(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/login":
			_, _ = writer.Write([]byte(`{"status":"success","data":{"token":"bearer-token"}}`))
		case "/people/6384/extended":
			if request.URL.Query().Get("meta") != "translations" {
				t.Errorf("missing translations metadata: %s", request.URL.RawQuery)
			}
			if request.Header.Get("Authorization") != "Bearer bearer-token" {
				t.Errorf("missing bearer token")
			}
			_, _ = writer.Write([]byte(`{"status":"success","data":{"id":6384,"name":"Example Person"}}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	client := New(config.TVDBConfig{APIKey: "tvdb-key", BaseURL: server.URL})
	payloads, err := client.CollectPerson(context.Background(), providers.Identifier{Provider: "tvdb", Namespace: "person", Value: "6384"})
	if err != nil {
		t.Fatal(err)
	}
	if len(payloads) != 1 || payloads[0].ProviderNamespace != "person" || payloads[0].ProviderRecordID != "6384" {
		t.Fatalf("payloads: %+v", payloads)
	}
}

package omdb

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

func TestCollectorForwardsAPIKeyWithoutPuttingItInRequestIdentity(t *testing.T) {
	t.Parallel()
	const secret = "caller-omdb-key"
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Query().Get("apikey") != secret || request.URL.Query().Get("i") != "tt0133093" || request.URL.Query().Get("plot") != "full" {
			t.Errorf("unexpected OMDb query: %s", request.URL.RawQuery)
		}
		_, _ = writer.Write([]byte(`{"Title":"The Matrix","imdbID":"tt0133093","Type":"movie","Response":"True"}`))
	}))
	defer server.Close()
	client := NewCached(config.OMDBConfig{BaseURL: server.URL}, nil, secret)
	payloads, err := client.Collect(context.Background(), providers.Identifier{Provider: "imdb", Namespace: "title", Value: "tt0133093"})
	if err != nil {
		t.Fatal(err)
	}
	if len(payloads) != 1 || strings.Contains(payloads[0].RequestKey, secret) {
		t.Fatalf("unsafe payload identity: %+v", payloads)
	}
}

func TestLogicalErrorsOverrideSharedReusePolicy(t *testing.T) {
	t.Parallel()
	tests := []struct {
		body string
		want time.Duration
	}{
		{`{"Response":"False","Error":"Movie not found!"}`, time.Hour},
		{`{"Response":"False","Error":"Invalid API key!"}`, 0},
		{`not-json`, 0},
	}
	for _, test := range tests {
		payload := providers.Payload{StatusCode: http.StatusOK, Body: []byte(test.body)}
		classifyReuse(&payload)
		if payload.ReuseDurationOverride == nil || *payload.ReuseDurationOverride != test.want {
			t.Fatalf("classification for %q: %+v", test.body, payload.ReuseDurationOverride)
		}
	}
}

package tvmaze

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/HeyaMedia/HeyaMetadata/internal/config"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers"
)

func TestExternalLookupUnlocksRichShowDetail(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/lookup/shows":
			if request.URL.Query().Get("imdb") != "tt0944947" {
				t.Errorf("lookup query: %s", request.URL.RawQuery)
			}
			_, _ = writer.Write([]byte(`{"id":82,"name":"Game of Thrones"}`))
		case "/shows/82":
			if len(request.URL.Query()["embed[]"]) != 6 {
				t.Errorf("embeds: %s", request.URL.RawQuery)
			}
			_, _ = writer.Write([]byte(`{"id":82,"name":"Game of Thrones","_embedded":{}}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	client := New(config.TVMazeConfig{BaseURL: server.URL, RequestsPerSecond: 1000})
	payloads, err := client.Collect(context.Background(), providers.Identifier{Provider: "imdb", Namespace: "title", Value: "tt0944947"})
	if err != nil {
		t.Fatal(err)
	}
	if len(payloads) != 2 || payloads[1].ProviderRecordID != "82" || !strings.Contains(payloads[1].RequestKey, "embed%5B%5D=episodes") {
		t.Fatalf("payloads: %+v", payloads)
	}
}

func TestPersonCollectionEmbedsShowsInCastAndCrewCredits(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/people/123":
			_, _ = writer.Write([]byte(`{"id":123,"name":"Example Person"}`))
		case "/people/123/castcredits", "/people/123/crewcredits":
			if request.URL.Query().Get("embed") != "show" {
				t.Errorf("missing embedded show: %s", request.URL.RawQuery)
			}
			_, _ = writer.Write([]byte(`[]`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	client := New(config.TVMazeConfig{BaseURL: server.URL, RequestsPerSecond: 1000})
	payloads, err := client.Collect(context.Background(), providers.Identifier{Provider: "tvmaze", Namespace: "person", Value: "123"})
	if err != nil {
		t.Fatal(err)
	}
	if len(payloads) != 3 || payloads[1].ProviderNamespace != "person_castcredits" || payloads[2].ProviderNamespace != "person_crewcredits" {
		t.Fatalf("payloads: %+v", payloads)
	}
}

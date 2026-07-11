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

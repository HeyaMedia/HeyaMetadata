package wikidata

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/HeyaMedia/HeyaMetadata/internal/config"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers"
)

func TestEntityAndSearchUseInformativeUserAgent(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("User-Agent") != "HeyaMetadata/test (test@example.com)" {
			t.Error("missing informative User-Agent")
		}
		if strings.HasPrefix(request.URL.Path, "/wiki/") {
			_, _ = writer.Write([]byte(`{"entities":{"Q42":{"id":"Q42"}}}`))
			return
		}
		_, _ = writer.Write([]byte(`{"success":1,"search":[]}`))
	}))
	defer server.Close()
	client := New(config.WikidataConfig{BaseURL: server.URL, RequestsPerSecond: 1000, UserAgent: "HeyaMetadata/test (test@example.com)"})
	if _, err := client.Collect(context.Background(), providers.Identifier{Provider: "wikidata", Namespace: "entity", Value: "q42"}); err != nil {
		t.Fatal(err)
	}
	search, err := client.Search(context.Background(), "Douglas Adams", "en", 10, 20)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(search.RequestKey, "continue=20") || !strings.Contains(search.RequestKey, "language=en") {
		t.Fatalf("search request identity: %s", search.RequestKey)
	}
}

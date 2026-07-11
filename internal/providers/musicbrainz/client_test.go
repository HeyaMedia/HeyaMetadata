package musicbrainz

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

func TestCollectorBuildsLookupWithRequiredUserAgent(t *testing.T) {
	t.Parallel()
	const mbid = "b10bbbfc-cf9e-42e0-be17-e2c3e1d2600d"
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/artist/"+mbid || request.URL.Query().Get("fmt") != "json" || !strings.Contains(request.URL.Query().Get("inc"), "url-rels") {
			t.Errorf("unexpected MusicBrainz request: %s", request.URL.String())
		}
		if request.Header.Get("User-Agent") != "HeyaMetadata/test (test@example.com)" {
			t.Errorf("missing user agent: %q", request.Header.Get("User-Agent"))
		}
		_, _ = writer.Write([]byte(`{"id":"` + mbid + `","name":"The Beatles"}`))
	}))
	defer server.Close()
	client := New(config.MusicBrainzConfig{BaseURL: server.URL, RequestsPerSecond: 1000, UserAgent: "HeyaMetadata/test (test@example.com)"})
	payloads, err := client.Collect(context.Background(), providers.Identifier{Provider: "musicbrainz", Namespace: "artist", Value: mbid})
	if err != nil {
		t.Fatal(err)
	}
	if len(payloads) != 1 || payloads[0].ProviderNamespace != "artist" || !strings.Contains(payloads[0].RequestKey, "inc=") {
		t.Fatalf("payload: %+v", payloads)
	}
}

func TestCollectorRejectsInvalidNamespaceAndMBID(t *testing.T) {
	t.Parallel()
	client := New(config.MusicBrainzConfig{BaseURL: "https://musicbrainz.org/ws/2", RequestsPerSecond: 1, UserAgent: "test/1 (test@example.com)"})
	for _, identifier := range []providers.Identifier{
		{Provider: "musicbrainz", Namespace: "label", Value: "b10bbbfc-cf9e-42e0-be17-e2c3e1d2600d"},
		{Provider: "musicbrainz", Namespace: "artist", Value: "not-an-mbid"},
	} {
		if _, err := client.Collect(context.Background(), identifier); err == nil {
			t.Fatalf("accepted invalid identifier: %+v", identifier)
		}
	}
}

func TestMalformedSuccessIsNotReusable(t *testing.T) {
	t.Parallel()
	payload := providers.Payload{StatusCode: http.StatusOK, Body: []byte(`{"id":"different"}`)}
	classifyReuse("b10bbbfc-cf9e-42e0-be17-e2c3e1d2600d")(&payload)
	if payload.ReuseDurationOverride == nil || *payload.ReuseDurationOverride != time.Duration(0) {
		t.Fatalf("classification: %+v", payload.ReuseDurationOverride)
	}
}

func TestSearchAndBrowseIncludePaginationInRequestIdentity(t *testing.T) {
	t.Parallel()
	const mbid = "b10bbbfc-cf9e-42e0-be17-e2c3e1d2600d"
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/artist/":
			_, _ = writer.Write([]byte(`{"artists":[],"count":0,"offset":25}`))
		case "/release-group":
			_, _ = writer.Write([]byte(`{"release-groups":[],"release-group-count":0,"release-group-offset":100}`))
		default:
			t.Errorf("unexpected path: %s", request.URL.Path)
		}
	}))
	defer server.Close()
	client := New(config.MusicBrainzConfig{BaseURL: server.URL, RequestsPerSecond: 1000, UserAgent: "HeyaMetadata/test (test@example.com)"})
	search, err := client.Search(context.Background(), "artist", "The Beatles", 10, 25)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(search.RequestKey, "limit=10") || !strings.Contains(search.RequestKey, "offset=25") || !strings.Contains(search.RequestKey, "query=The+Beatles") {
		t.Fatalf("search request key: %s", search.RequestKey)
	}
	browse, err := client.BrowseReleaseGroups(context.Background(), mbid, 50, 100)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(browse.RequestKey, "limit=50") || !strings.Contains(browse.RequestKey, "offset=100") {
		t.Fatalf("browse request key: %s", browse.RequestKey)
	}
}

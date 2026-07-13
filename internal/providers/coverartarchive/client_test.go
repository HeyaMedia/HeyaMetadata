package coverartarchive

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/HeyaMedia/HeyaMetadata/internal/config"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers"
)

func TestCollectorUsesTypedMusicBrainzReleaseGroupIdentity(t *testing.T) {
	t.Parallel()
	const mbid = "9162580e-5df4-32de-80cc-f45a8d8a9b1d"
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/release-group/"+mbid || request.Header.Get("User-Agent") != "HeyaMetadata/test" {
			t.Errorf("unexpected request: %s", request.URL.String())
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"images":[]}`))
	}))
	defer server.Close()
	client := New(config.CoverArtArchiveConfig{BaseURL: server.URL, RequestsPerSecond: 1000, UserAgent: "HeyaMetadata/test"})
	payloads, err := client.Collect(context.Background(), providers.Identifier{Provider: "musicbrainz", Namespace: "release_group", Value: mbid})
	if err != nil {
		t.Fatal(err)
	}
	if len(payloads) != 1 || payloads[0].Provider != "coverartarchive" || payloads[0].RequestKey != "release-group/"+mbid {
		t.Fatalf("payloads: %+v", payloads)
	}
}

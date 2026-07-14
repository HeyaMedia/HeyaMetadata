package animelists

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/HeyaMedia/HeyaMetadata/internal/config"
)

func TestTVDBLookupPrefersSeriesRootRegardlessOfDumpOrder(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`[
			{"anidb_id":10944,"tvdb_id":267440,"season":{"tvdb":2}},
			{"anidb_id":9999,"tvdb_id":267440,"season":{"tvdb":1},"episode_offset":{"tvdb":12}},
			{"anidb_id":9541,"tvdb_id":267440,"season":{"tvdb":1}}
		]`))
	}))
	t.Cleanup(server.Close)

	client := New(config.AnimeListsConfig{URL: server.URL, UserAgent: "test"})
	_, entry, found, err := client.LookupExternal(context.Background(), "tvdb", "267440")
	if err != nil || !found {
		t.Fatalf("found=%t err=%v", found, err)
	}
	if entry.AniDBID != 9541 || !entry.IsTVDBSeriesRoot() {
		t.Fatalf("entry=%+v", entry)
	}
}

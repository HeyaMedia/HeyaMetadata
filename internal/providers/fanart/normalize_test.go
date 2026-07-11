package fanart

import (
	"testing"
	"time"
)

func TestNormalizePreservesArtworkSemantics(t *testing.T) {
	t.Parallel()
	body := []byte(`{
		"name":"The Matrix","tmdb_id":"603","imdb_id":"tt0133093",
		"movieposter":[{"id":"1","url":"http://assets.fanart.tv/poster.jpg","lang":"en","likes":"42","width":"1000","height":"1500"}],
		"hdmovielogo":[{"id":"2","url":"https://assets.fanart.tv/logo.png","lang":"00","likes":"7","width":"800","height":"310"}],
		"moviebackground":[{"id":"3","url":"https://assets.fanart.tv/background.jpg","lang":"","likes":"9","width":"1920","height":"1080"}]
	}`)
	record, err := Normalize(body, "observation", time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	if record.ProviderRecord.Provider != "fanart" || len(record.IdentityCandidates) != 2 || len(record.Images) != 3 {
		t.Fatalf("record: %+v", record)
	}
	poster := record.Images[0]
	if poster.Class != "poster" || poster.SourceURL != "https://assets.fanart.tv/poster.jpg" || poster.Width != 1000 || poster.Height != 1500 || poster.Likes != 42 || poster.ProviderScore != 42 {
		t.Fatalf("poster: %+v", poster)
	}
	if record.Images[1].Class != "backdrop" || record.Images[2].Class != "logo" || record.Images[2].Language != "" {
		t.Fatalf("artwork classes/language: %+v", record.Images)
	}
}

func TestNormalizeRejectsMissingTMDBIdentity(t *testing.T) {
	t.Parallel()
	if _, err := Normalize([]byte(`{"name":"Unknown"}`), "observation", time.Now()); err == nil {
		t.Fatal("expected invalid identity error")
	}
}

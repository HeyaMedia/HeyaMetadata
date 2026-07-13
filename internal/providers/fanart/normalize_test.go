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

func TestNormalizeTVPreservesShowAndSeasonArtworkSemantics(t *testing.T) {
	t.Parallel()
	body := []byte(`{
		"name":"Game of Thrones","thetvdb_id":"121361",
		"tvposter":[{"id":"1","url":"http://assets.fanart.tv/poster.jpg","lang":"en","likes":"42","width":"1000","height":"1426"}],
		"hdtvlogo":[{"id":"2","url":"https://assets.fanart.tv/logo.png","lang":"00","likes":"7","width":"800","height":"310"}],
		"characterart":[{"id":"3","url":"https://assets.fanart.tv/character.png","lang":"","likes":"3","width":"512","height":"512"}],
		"seasonposter":[
			{"id":"4","season":"2","url":"https://assets.fanart.tv/s2-en.jpg","lang":"en","likes":"28","width":"1000","height":"1426"},
			{"id":"5","season":"2","url":"https://assets.fanart.tv/s2-de.jpg","lang":"de","likes":"6","width":"1000","height":"1426"},
			{"id":"6","season":null,"url":"https://assets.fanart.tv/all.jpg","lang":"en","likes":"1","width":"1000","height":"1426"}
		],
		"seasonbanner":[{"id":"7","season":"2","url":"https://assets.fanart.tv/s2-banner.jpg","lang":"en","likes":"4","width":"1000","height":"185"}],
		"seasonthumb":[{"id":"8","season":"2","url":"https://assets.fanart.tv/s2-thumb.jpg","lang":"en","likes":"4","width":"1000","height":"562"}]
	}`)
	record, err := NormalizeTV(body, "observation", time.Unix(1, 0).UTC(), "tv_show")
	if err != nil {
		t.Fatal(err)
	}
	if record.Provider != "fanart" || record.ProviderID != "121361" || len(record.Images) != 4 {
		t.Fatalf("record: %+v", record)
	}
	if record.Images[0].Class != "poster" || record.Images[1].Class != "logo" || record.Images[1].Language != "" || record.Images[2].Class != "characterart" {
		t.Fatalf("show images: %+v", record.Images)
	}
	if len(record.Seasons) != 1 || record.Seasons[0].Number != 2 || len(record.Seasons[0].Images) != 4 {
		t.Fatalf("seasons: %+v", record.Seasons)
	}
	if record.Seasons[0].Images[0].Language != "en" || record.Seasons[0].Images[1].Language != "de" || record.Seasons[0].Images[2].Class != "banner" || record.Seasons[0].Images[3].Class != "thumb" {
		t.Fatalf("season images: %+v", record.Seasons[0].Images)
	}
}

func TestNormalizeMusicArtistPreservesEveryArtistArtworkClass(t *testing.T) {
	t.Parallel()
	body := []byte(`{
		"name":"Ado","mbid_id":"e134b52f-2e9e-4734-9bc3-bea9648d1fa1",
		"artistbackground":[{"id":"1","url":"https://assets.fanart.tv/background.jpg","lang":"00","likes":"5","width":"1920","height":"1080"}],
		"artistthumb":[{"id":"2","url":"https://assets.fanart.tv/thumb.jpg","lang":"ja","likes":"4","width":"1000","height":"1000"}],
		"hdmusiclogo":[{"id":"3","url":"https://assets.fanart.tv/logo.png","lang":"en","likes":"3","width":"800","height":"310"}],
		"musicbanner":[{"id":"4","url":"https://assets.fanart.tv/banner.jpg","lang":"en","likes":"2","width":"1000","height":"185"}]
	}`)
	record, err := NormalizeMusicArtist(body, "observation", time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	if len(record.Images) != 4 || record.Images[0].Class != "backdrop" || record.Images[1].Class != "profile" || record.Images[2].Class != "logo" || record.Images[3].Class != "banner" {
		t.Fatalf("images: %+v", record.Images)
	}
	if record.Images[0].Language != "" || record.Images[1].Language != "ja" || record.Images[0].ProviderScore != 5 {
		t.Fatalf("image metadata: %+v", record.Images)
	}
}

func TestNormalizeMusicReleaseGroupMatchesMBIDWithoutTitleGuessing(t *testing.T) {
	t.Parallel()
	body := []byte(`{"name":"Ado","mbid_id":"e134b52f-2e9e-4734-9bc3-bea9648d1fa1","albums":[{"release_group_id":"0a5b4fcd-05d9-4c76-87bf-39fa6110a736","albumcover":[{"id":"1","url":"https://assets.fanart.tv/cover.jpg","likes":"5","width":"1000","height":"1000"}],"cdart":[{"id":"2","url":"https://assets.fanart.tv/disc.png","likes":"2","width":"1000","height":"1000"}]}]}`)
	record, err := NormalizeMusicReleaseGroup(body, "0a5b4fcd-05d9-4c76-87bf-39fa6110a736", "observation", time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	if len(record.Images) != 2 || record.Images[0].Class != "cover" || record.Images[1].Class != "disc" || record.Images[0].ProviderScore != 5 {
		t.Fatalf("images: %+v", record.Images)
	}
}

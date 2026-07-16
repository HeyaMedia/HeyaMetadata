package audiodb

import (
	"testing"
	"time"
)

const artistBody = `{"artists":[{
	"idArtist":"112024",
	"strArtist":"The Weeknd",
	"strMusicBrainzID":"C8B03190-306C-4120-BB0B-6F2EBFC06EA9",
	"strBiography":"Abel Tesfaye is a Canadian R&B singer.",
	"strBiographyDE":"Abel Tesfaye ist ein kanadischer Musiker.",
	"strGenre":"R&B",
	"strStyle":"Urban/R&B",
	"strMood":"Intense",
	"strGender":"Male",
	"strCountry":"Toronto, Canada",
	"strCountryCode":"CA",
	"intFormedYear":"2010",
	"strWebsite":"www.the-weeknd.com",
	"strFacebook":"www.facebook.com/theweeknd",
	"strTwitter":"1",
	"intPopularity":"97",
	"intFollowers":"102484717",
	"strArtistThumb":"https://r2.theaudiodb.com/images/thumb.jpg",
	"strArtistLogo":"https://r2.theaudiodb.com/images/logo.png",
	"strArtistBanner":"https://r2.theaudiodb.com/images/banner.jpg",
	"strArtistFanart":"https://r2.theaudiodb.com/images/fanart.jpg"
}]}`

func TestNormalizeArtist(t *testing.T) {
	t.Parallel()
	record, err := NormalizeArtist([]byte(artistBody), weekndMBID, "obs-1", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if record.ProviderRecord.Provider != "audiodb" || record.ProviderRecord.Value != weekndMBID {
		t.Fatalf("provider record: %+v", record.ProviderRecord)
	}
	if len(record.IdentityCandidates) != 1 || record.IdentityCandidates[0].Provider != "musicbrainz" || record.IdentityCandidates[0].NormalizedValue != weekndMBID {
		t.Fatalf("identity candidates: %+v", record.IdentityCandidates)
	}
	if len(record.Names) != 1 || record.Names[0].Value != "The Weeknd" {
		t.Fatalf("names: %+v", record.Names)
	}
	if len(record.Biographies) != 2 || record.Biographies[0].Language != "en" || record.Biographies[1].Language != "de" {
		t.Fatalf("biographies: %+v", record.Biographies)
	}
	if record.Classification.Gender != "male" {
		t.Fatalf("classification: %+v", record.Classification)
	}
	if len(record.Genres) != 1 || record.Genres[0].Name != "R&B" || len(record.Tags) != 2 {
		t.Fatalf("genres/tags: %+v %+v", record.Genres, record.Tags)
	}
	if len(record.Areas) != 1 || record.Areas[0].ISOCodes[0] != "CA" || record.Areas[0].Role != "country" {
		t.Fatalf("areas: %+v", record.Areas)
	}
	if len(record.Lifecycle.Dates) != 1 || record.Lifecycle.Dates[0].Value != "2010" || record.Lifecycle.Dates[0].Type != "begin" {
		t.Fatalf("lifecycle: %+v", record.Lifecycle)
	}
	// strTwitter carries a junk placeholder ("1") and must be dropped.
	if len(record.Links) != 2 || record.Links[0].URL != "https://www.the-weeknd.com" || record.Links[1].Type != "facebook" {
		t.Fatalf("links: %+v", record.Links)
	}
	if len(record.Metrics) != 2 || record.Metrics[0].Name != "popularity" || record.Metrics[0].Value != 97 {
		t.Fatalf("metrics: %+v", record.Metrics)
	}
	if len(record.Images) != 4 || record.Images[0].Class != "profile" || record.Images[3].Class != "backdrop" {
		t.Fatalf("images: %+v", record.Images)
	}
}

func TestNormalizeArtistRejectsIdentityMismatch(t *testing.T) {
	t.Parallel()
	if _, err := NormalizeArtist([]byte(artistBody), "11111111-1111-4111-8111-111111111111", "obs-1", time.Now()); err == nil {
		t.Fatal("expected mismatched MusicBrainz identity to be rejected")
	}
	if _, err := NormalizeArtist([]byte(`{"artists":null}`), weekndMBID, "obs-1", time.Now()); err == nil {
		t.Fatal("expected empty response to be rejected")
	}
}

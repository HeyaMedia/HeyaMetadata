package bandcamp

import (
	"testing"
	"time"
)

const albumPage = `<html><head>
<script type="application/ld+json">
{
  "@type": "MusicAlbum",
  "@id": "https://kinggizzard.bandcamp.com/album/flight-b741",
  "name": "Flight b741",
  "datePublished": "09 Aug 2024 00:00:00 GMT",
  "numTracks": 10,
  "image": "https://f4.bcbits.com/img/a3548545002_10.jpg",
  "keywords": ["Alternative", "Garage"],
  "albumReleaseType": "AlbumRelease",
  "byArtist": {"@type": "MusicGroup", "name": "King Gizzard & The Lizard Wizard", "@id": "https://kinggizzard.bandcamp.com"},
  "albumRelease": [{"@type": ["MusicRelease", "Product"], "musicReleaseFormat": "DigitalFormat"}],
  "track": {"@type": "ItemList", "numberOfItems": 10, "itemListElement": [
    {"@type": "ListItem", "position": 1, "item": {"@id": "https://kinggizzard.bandcamp.com/track/mirage-city", "name": "Mirage City", "duration": "P00H04M48S"}},
    {"@type": "ListItem", "position": 2, "item": {"@id": "https://kinggizzard.bandcamp.com/track/antarctica", "name": "Antarctica", "duration": "P00H05M06S"}}
  ]}
}
</script>
</head><body><div data-band="{&quot;id&quot;:1,&quot;name&quot;:&quot;x&quot;}"></div></body></html>`

func TestNormalizeAlbumFromJSONLD(t *testing.T) {
	t.Parallel()
	record, err := NormalizeAlbum([]byte(albumPage), "kinggizzard/flight-b741", "obs-1", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if record.ProviderRecord.Provider != "bandcamp" || record.ProviderRecord.Value != "kinggizzard/flight-b741" {
		t.Fatalf("provider record: %+v", record.ProviderRecord)
	}
	if len(record.Titles) != 1 || record.Titles[0].Value != "Flight b741" {
		t.Fatalf("titles: %+v", record.Titles)
	}
	if record.Classification.PrimaryType != "album" {
		t.Fatalf("classification: %+v", record.Classification)
	}
	if len(record.Dates) != 1 || record.Dates[0].Value != "2024-08-09" || record.Dates[0].Precision != "day" {
		t.Fatalf("dates: %+v", record.Dates)
	}
	if len(record.ArtistCredits) != 1 || record.ArtistCredits[0].ArtistID != "kinggizzard" {
		t.Fatalf("credits: %+v", record.ArtistCredits)
	}
	if len(record.Tags) != 2 || record.Tags[0].Name != "Alternative" {
		t.Fatalf("tags: %+v", record.Tags)
	}
	if len(record.Tracks) != 2 || record.Tracks[0].Title != "Mirage City" || record.Tracks[0].DurationMS != 288000 {
		t.Fatalf("tracks: %+v", record.Tracks)
	}
	if len(record.Editions) != 1 || record.Editions[0].TrackCount != 10 || record.Editions[0].Formats[0] != "Digital" || record.Editions[0].Image == nil {
		t.Fatalf("edition: %+v", record.Editions[0])
	}
	if len(record.Images) != 1 || record.Images[0].Class != "cover" {
		t.Fatalf("images: %+v", record.Images)
	}
}

func TestNormalizeAlbumRejectsMismatchedIdentity(t *testing.T) {
	t.Parallel()
	if _, err := NormalizeAlbum([]byte(albumPage), "someoneelse/flight-b741", "obs-1", time.Now()); err == nil {
		t.Fatal("expected mismatched subdomain to be rejected")
	}
	if _, err := NormalizeAlbum([]byte(albumPage), "kinggizzard/other-album", "obs-1", time.Now()); err == nil {
		t.Fatal("expected mismatched slug to be rejected")
	}
	if _, err := NormalizeAlbum([]byte(`<html>no json-ld</html>`), "kinggizzard/flight-b741", "obs-1", time.Now()); err == nil {
		t.Fatal("expected a page without JSON-LD to be rejected")
	}
}

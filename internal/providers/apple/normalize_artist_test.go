package apple

import (
	"testing"
	"time"
)

func TestNormalizeArtistSupportsITunesAndCatalogResponses(t *testing.T) {
	t.Parallel()
	for name, body := range map[string]string{
		"itunes":  `{"resultCount":2,"results":[{"wrapperType":"artist","artistName":"The Beatles","artistId":136975,"artistType":"Artist","artistLinkUrl":"https://music.apple.com/gb/artist/136975","primaryGenreName":"Rock","primaryGenreId":21},{"wrapperType":"collection","artistId":136975}]}`,
		"catalog": `{"data":[{"id":"136975","type":"artists","attributes":{"name":"The Beatles","genreNames":["Rock","Pop"],"url":"https://music.apple.com/gb/artist/136975"}}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			record, err := NormalizeArtist([]byte(body), "136975", "observation", time.Unix(1, 0).UTC())
			if err != nil {
				t.Fatal(err)
			}
			if record.ProviderRecord.Value != "136975" || record.Names[0].Value != "The Beatles" || len(record.Genres) == 0 {
				t.Fatalf("record: %+v", record)
			}
		})
	}
}

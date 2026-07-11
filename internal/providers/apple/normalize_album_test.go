package apple

import (
	"strings"
	"testing"
	"time"
)

func TestNormalizeAlbumKeepsStorefrontEditionAndTracks(t *testing.T) {
	body := []byte(`{"resultCount":3,"results":[{"wrapperType":"collection","collectionId":1441164426,"collectionName":"Abbey Road (Remastered)","artistId":136975,"artistName":"The Beatles","collectionViewUrl":"https://music.apple.com/album/1441164426","artworkUrl100":"https://is1-ssl.mzstatic.com/cover.jpg","collectionExplicitness":"notExplicit","trackCount":2,"country":"GBR","releaseDate":"1969-09-26T07:00:00Z","primaryGenreName":"Rock"},{"wrapperType":"track","collectionId":1441164426,"trackId":1441164430,"trackName":"Come Together","trackNumber":1,"discNumber":1,"trackTimeMillis":258947,"artistId":136975,"artistName":"The Beatles"},{"wrapperType":"track","collectionId":999,"trackId":1,"trackName":"Wrong Album"}]}`)
	record, err := NormalizeAlbum(body, "1441164426", "observation", time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	if record.ProviderRecord.Value != "1441164426" || record.Editions[0].Country != "GBR" || record.Dates[0].Value != "1969-09-26" {
		t.Fatalf("record: %+v", record)
	}
	if len(record.Tracks) != 1 || record.Tracks[0].ProviderID != "1441164430" || record.ArtistCredits[0].ArtistID != "136975" {
		t.Fatalf("tracks/credits: %+v / %+v", record.Tracks, record.ArtistCredits)
	}
}

func TestNormalizeAlbumSupportsAppleMusicCatalog(t *testing.T) {
	body := []byte(`{"data":[{"id":"1441164426","type":"albums","attributes":{"name":"Abbey Road (Remastered)","artistName":"The Beatles","url":"https://music.apple.com/album/1441164426","upc":"602547670342","releaseDate":"1969-09-26","trackCount":1,"genreNames":["Rock"],"artwork":{"url":"https://is1-ssl.mzstatic.com/{w}x{h}.jpg","width":3000,"height":3000}},"relationships":{"artists":{"data":[{"id":"136975"}]},"tracks":{"data":[{"id":"1441164430","type":"songs","attributes":{"name":"Come Together","durationInMillis":258947,"trackNumber":1,"discNumber":1}}]}}}]}`)
	record, err := NormalizeAlbum(body, "1441164426", "observation", time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	if record.ArtistCredits[0].ArtistID != "136975" || len(record.Tracks) != 1 || !strings.Contains(record.Images[0].SourceURL, "1200x1200") {
		t.Fatalf("record: %+v", record)
	}
}

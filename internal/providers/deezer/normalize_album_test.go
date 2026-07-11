package deezer

import (
	"testing"
	"time"
)

func TestNormalizeAlbumPreservesCatalogEdition(t *testing.T) {
	body := []byte(`{"id":12047952,"title":"Abbey Road (Remastered)","upc":"602547670342","link":"https://www.deezer.com/album/12047952","cover_xl":"https://cdn-images.dzcdn.net/cover.jpg","release_date":"2015-12-24","record_type":"album","explicit_lyrics":false,"duration":2832,"nb_tracks":17,"fans":196621,"genres":{"data":[{"id":152,"name":"Rock"}]},"contributors":[{"id":1,"name":"The Beatles","role":"Main"}],"tracks":{"data":[{"id":116348452,"title":"Come Together (Remastered 2009)","title_short":"Come Together","duration":258,"artist":{"id":1,"name":"The Beatles"}}]}}`)
	record, err := NormalizeAlbum(body, "observation", time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	if record.Editions[0].Barcode != "602547670342" || record.Editions[0].DurationMS != 2832000 || record.Metrics[0].Value != 196621 {
		t.Fatalf("record: %+v", record)
	}
	if len(record.Tracks) != 1 || record.Tracks[0].ArtistCredits[0].ArtistID != "1" {
		t.Fatalf("tracks: %+v", record.Tracks)
	}
}

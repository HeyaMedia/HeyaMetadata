package deezer

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestNormalizeAlbumPreservesCatalogEdition(t *testing.T) {
	body := []byte(`{"id":12047952,"title":"Abbey Road (Remastered)","upc":"602547670342","link":"https://www.deezer.com/album/12047952","cover_xl":"https://cdn-images.dzcdn.net/cover.jpg","release_date":"2015-12-24","record_type":"album","explicit_lyrics":false,"duration":2832,"nb_tracks":17,"fans":196621,"label":"Universal Music Catalogue","genres":{"data":[{"id":152,"name":"Rock"}]},"contributors":[{"id":1,"name":"The Beatles","role":"Main"}],"tracks":{"data":[{"id":116348452,"title":"Come Together (Remastered 2009)","title_short":"Come Together","duration":258,"disk_number":2,"preview":"https://cdnt-preview.dzcdn.net/preview.mp3?token=secret","artist":{"id":1,"name":"The Beatles"}}]}}`)
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
	if record.Tracks[0].DiscNumber != 2 {
		t.Fatalf("disc number: %+v", record.Tracks[0])
	}
	if record.ArtistCredits[0].Role != "main" {
		t.Fatalf("contributor role: %+v", record.ArtistCredits)
	}
	if len(record.Editions[0].Labels) != 1 || record.Editions[0].Labels[0].Name != "Universal Music Catalogue" {
		t.Fatalf("labels: %+v", record.Editions[0].Labels)
	}
	if record.Tracks[0].PreviewURL == "" {
		t.Fatal("preview URL was not parsed")
	}
	encoded, _ := json.Marshal(record)
	if strings.Contains(string(encoded), "token=secret") {
		t.Fatal("signed preview URL entered normalized JSON")
	}
}

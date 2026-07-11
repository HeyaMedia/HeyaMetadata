package deezer

import (
	"testing"
	"time"
)

func TestNormalizeArtistPreservesMetricsAndArtwork(t *testing.T) {
	record, err := NormalizeArtist([]byte(`{"id":1,"name":"The Beatles","link":"https://www.deezer.com/artist/1","picture_xl":"https://img/1.jpg","nb_album":45,"nb_fan":8076012,"type":"artist"}`), "observation", time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	if len(record.Metrics) != 2 || record.Metrics[1].Value != 8076012 || len(record.Images) != 1 {
		t.Fatalf("record: %+v", record)
	}
}

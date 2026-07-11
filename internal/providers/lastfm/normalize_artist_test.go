package lastfm

import (
	"testing"
	"time"
)

func TestNormalizeArtistRequiresMatchingMBIDAndPreservesAudienceEvidence(t *testing.T) {
	const mbid = "b10bbbfc-cf9e-42e0-be17-e2c3e1d2600d"
	record, err := NormalizeArtist([]byte(`{"artist":{"name":"The Beatles","mbid":"`+mbid+`","url":"https://last.fm/music/The+Beatles","image":[{"#text":"https://img/large.jpg","size":"large"},{"#text":"https://img/large.jpg","size":"extralarge"}],"stats":{"listeners":"6607080","playcount":"1125559838"},"similar":{"artist":[{"name":"John Lennon","mbid":"","url":"https://last.fm/john"}]},"tags":{"tag":[{"name":"classic rock"}]},"bio":{"content":"A long biography"}}}`), mbid, "observation", time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	if len(record.Metrics) != 2 || len(record.Images) != 1 || len(record.SimilarArtists) != 1 || record.SimilarArtists[0].ProviderID != "" {
		t.Fatalf("record: %+v", record)
	}
	if _, err := NormalizeArtist([]byte(`{"artist":{"name":"Wrong","mbid":"00000000-0000-0000-0000-000000000001"}}`), mbid, "observation", time.Now()); err == nil {
		t.Fatal("expected identity mismatch")
	}
}

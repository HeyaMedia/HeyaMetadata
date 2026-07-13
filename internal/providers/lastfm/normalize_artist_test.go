package lastfm

import (
	"testing"
	"time"
)

func TestNormalizeArtistRequiresMatchingMBIDAndPreservesAudienceEvidence(t *testing.T) {
	const mbid = "b10bbbfc-cf9e-42e0-be17-e2c3e1d2600d"
	record, err := NormalizeArtist([]byte(`{"artist":{"name":"The Beatles","mbid":"`+mbid+`","url":"https://last.fm/music/The+Beatles","image":[{"#text":"https://img/large.jpg","size":"large"},{"#text":"https://img/large.jpg","size":"extralarge"}],"stats":{"listeners":"6607080","playcount":"1125559838"},"similar":{"artist":[{"name":"John Lennon","mbid":"","url":"https://last.fm/john"}]},"tags":{"tag":[{"name":"classic rock"}]},"bio":{"content":"A long biography"}}}`), mbid, []string{"The Beatles"}, "observation", time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	if len(record.Metrics) != 2 || len(record.Images) != 0 || len(record.SimilarArtists) != 1 || record.SimilarArtists[0].ProviderID != "" {
		t.Fatalf("record: %+v", record)
	}
	if _, err := NormalizeArtist([]byte(`{"artist":{"name":"Wrong","mbid":"00000000-0000-0000-0000-000000000001"}}`), mbid, []string{"The Beatles"}, "observation", time.Now()); err == nil {
		t.Fatal("expected identity mismatch")
	}
}

func TestNormalizeArtistRetainsNameScopedAggregateWithoutClaimingWrongMBID(t *testing.T) {
	const expected = "e134b52f-2e9e-4734-9bc3-bea9648d1fa1"
	record, err := NormalizeArtist([]byte(`{"artist":{"name":"Ado","mbid":"ca195d97-30d9-4870-8a52-7fd3c2e175c3","stats":{"listeners":"740395"}}}`), expected, []string{"Ado"}, "observation", time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	if record.ProviderRecord.Value != expected || len(record.IdentityCandidates) != 0 || len(record.Warnings) != 1 || len(record.Metrics) != 1 {
		t.Fatalf("record: %+v", record)
	}
}

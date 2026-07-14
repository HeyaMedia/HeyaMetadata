package musicbrainz

import (
	"testing"
	"time"
)

func TestNormalizeRecordingPreservesRecordingEvidence(t *testing.T) {
	body := []byte(`{"id":"9edb3d47-4f3f-4f5f-8f2e-73f78b0d0d32","title":"Come Together","length":259000,"disambiguation":"original mix","video":false,"isrcs":["GBAYE0601690"],"artist-credit":[{"name":"The Beatles","artist":{"id":"b10bbbfc-cf9e-42e0-be17-e2c3e1d2600d","name":"The Beatles"}}],"genres":[{"id":"11111111-1111-4111-8111-111111111111","name":"rock","count":4}],"tags":[{"name":"classic rock","count":3}],"rating":{"value":4.8,"votes-count":12},"releases":[{"id":"31765b9f-e969-4257-855f-c7ea1f657b2a","title":"Abbey Road","status":"Official","date":"1969-09-26","country":"GB","release-group":{"id":"9162580e-5df4-32de-80cc-f45a8d8a9b1d","title":"Abbey Road"}}],"relations":[{"type":"lyrics","url":{"resource":"https://example.com/lyrics"}},{"type":"performance","attributes":["live"],"work":{"id":"e17379a1-650f-4f58-87bb-13add51c7568","title":"Come Together","language":"eng"}}]}`)
	record, err := NormalizeRecording(body, "observation", time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	value := record.Recording
	if value.ProviderID == "" || len(value.ISRCs) != 1 || len(value.ArtistCredits) != 1 || len(value.Releases) != 1 || len(value.Genres) != 1 || len(value.Tags) != 1 || value.Rating == nil || len(value.Links) != 1 || len(record.WorkRelations) != 1 || record.WorkRelations[0].Attributes[0] != "live" {
		t.Fatalf("recording: %+v", value)
	}
}

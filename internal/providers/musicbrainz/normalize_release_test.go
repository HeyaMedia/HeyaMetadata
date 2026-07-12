package musicbrainz

import (
	"testing"
	"time"
)

func TestNormalizeReleasePreservesMediaTrackAndRecordingBoundaries(t *testing.T) {
	body := []byte(`{"id":"34e7ff03-8160-4d4f-a407-03f2c6510a2e","title":"Abbey Road","status":"Official","date":"1969-09-26","country":"GB","barcode":"094638246824","release-group":{"id":"9162580e-5df4-32de-80cc-f45a8d8a9b1d"},"artist-credit":[{"name":"The Beatles","artist":{"id":"b10bbbfc-cf9e-42e0-be17-e2c3e1d2600d","name":"The Beatles"}}],"label-info":[{"catalog-number":"PCS 7088","label":{"id":"b4cca2a7-220d-4a0b-87b3-6c50735353e3","name":"Apple Records"}}],"media":[{"position":1,"format":"12\" Vinyl","track-count":1,"discs":[{"id":"disc"}],"tracks":[{"id":"11111111-1111-4111-8111-111111111111","position":1,"number":"A1","title":"Come Together","length":259000,"recording":{"id":"22222222-2222-4222-8222-222222222222","title":"Come Together","length":259000,"isrcs":["gbayE0601690"],"artist-credit":[{"name":"The Beatles","artist":{"id":"b10bbbfc-cf9e-42e0-be17-e2c3e1d2600d","name":"The Beatles"}}]}}]}]}`)
	r, err := NormalizeRelease(body, "obs", time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Media) != 1 || len(r.Media[0].Tracks) != 1 || r.Media[0].Tracks[0].Recording.ProviderID != "22222222-2222-4222-8222-222222222222" || r.Media[0].Tracks[0].Recording.ISRCs[0] != "GBAYE0601690" {
		t.Fatalf("record: %+v", r)
	}
}

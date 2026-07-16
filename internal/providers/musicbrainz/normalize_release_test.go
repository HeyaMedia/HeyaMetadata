package musicbrainz

import (
	"testing"
	"time"
)

func TestNormalizeReleasePreservesMediaTrackAndRecordingBoundaries(t *testing.T) {
	body := []byte(`{"id":"34e7ff03-8160-4d4f-a407-03f2c6510a2e","title":"Abbey Road","status":"Official","date":"1969-09-26","country":"GB","barcode":"094638246824","release-group":{"id":"9162580e-5df4-32de-80cc-f45a8d8a9b1d"},"artist-credit":[{"name":"The Beatles","artist":{"id":"b10bbbfc-cf9e-42e0-be17-e2c3e1d2600d","name":"The Beatles"}}],"label-info":[{"catalog-number":"PCS 7088","label":{"id":"b4cca2a7-220d-4a0b-87b3-6c50735353e3","name":"Apple Records"}}],"media":[{"position":1,"format":"12\" Vinyl","track-count":1,"discs":[{"id":"disc"}],"tracks":[{"id":"11111111-1111-4111-8111-111111111111","position":1,"number":"A1","title":"Come Together","length":259000,"recording":{"id":"22222222-2222-4222-8222-222222222222","title":"Come Together","length":259000,"isrcs":["gbayE0601690"],"artist-credit":[{"name":"The Beatles","artist":{"id":"b10bbbfc-cf9e-42e0-be17-e2c3e1d2600d","name":"The Beatles"}}],"relations":[{"type":"performance","work":{"id":"e17379a1-650f-4f58-87bb-13add51c7568","title":"Come Together","language":"eng"}},{"type":"producer","target-type":"artist","artist":{"id":"0dd23217-b45e-4f4b-9d24-2e6b91410a9f","name":"George Martin"}},{"type":"instrument","target-type":"artist","attributes":["bass guitar"],"artist":{"id":"ba550d0e-adac-4864-b88b-407cab5e76af","name":"Paul McCartney"}}]}}]}]}`)
	r, err := NormalizeRelease(body, "obs", time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Media) != 1 || len(r.Media[0].Tracks) != 1 || r.Media[0].Tracks[0].Recording.ProviderID != "22222222-2222-4222-8222-222222222222" || r.Media[0].Tracks[0].Recording.ISRCs[0] != "GBAYE0601690" || len(r.Media[0].Tracks[0].WorkRelations) != 1 {
		t.Fatalf("record: %+v", r)
	}
	credits := r.Media[0].Tracks[0].Recording.Credits
	if len(credits) != 2 || credits[0].Role != "instrument" || credits[0].Attributes[0] != "bass_guitar" || credits[1].Role != "producer" || credits[1].ArtistName != "George Martin" {
		t.Fatalf("performance credits: %+v", credits)
	}
}

func TestNormalizeReleaseKeepsEventsGenresTagsLinksAndTextRepresentation(t *testing.T) {
	body := []byte(`{"id":"34e7ff03-8160-4d4f-a407-03f2c6510a2e","title":"Abbey Road","status":"Official","date":"1969-09-26","country":"GB","barcode":"094638246824","asin":"B0025KVLTC",
		"text-representation":{"language":"eng","script":"Latn"},
		"release-events":[{"date":"1969-09-26","area":{"iso-3166-1-codes":["GB"]}},{"date":"1969-10-01","area":{"iso-3166-1-codes":["US"]}}],
		"genres":[{"id":"rock","name":"rock","count":9}],
		"tags":[{"name":"classic","count":4}],
		"relations":[{"type":"discogs","url":{"resource":"https://www.discogs.com/release/2607562"}}],
		"media":[]}`)
	r, err := NormalizeRelease(body, "obs", time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	if r.ASIN != "B0025KVLTC" || r.Language != "eng" || r.Script != "Latn" {
		t.Fatalf("text representation: %+v", r)
	}
	if len(r.ReleaseEvents) != 2 || r.ReleaseEvents[1].Country != "US" || r.ReleaseEvents[1].Date != "1969-10-01" {
		t.Fatalf("release events: %+v", r.ReleaseEvents)
	}
	if len(r.Genres) != 1 || r.Genres[0].Name != "rock" || r.Genres[0].Count != 9 || len(r.Tags) != 1 || r.Tags[0].Name != "classic" {
		t.Fatalf("genres/tags: %+v %+v", r.Genres, r.Tags)
	}
	if len(r.Links) != 1 || r.Links[0].Type != "discogs" || r.Links[0].URL != "https://www.discogs.com/release/2607562" {
		t.Fatalf("links: %+v", r.Links)
	}
}

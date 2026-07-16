package musicbrainz

import (
	"testing"
	"time"
)

func TestNormalizeReleaseGroupPreservesWorkAndEditionBoundaries(t *testing.T) {
	t.Parallel()
	body := []byte(`{"id":"9162580e-5df4-32de-80cc-f45a8d8a9b1d","title":"Abbey Road","primary-type":"Album","secondary-types":[],"first-release-date":"1969-09-26","aliases":[{"name":"Abbey Road: Deluxe Edition","sort-name":"Abbey Road: Deluxe Edition"}],"artist-credit":[{"name":"The Beatles","joinphrase":"","artist":{"id":"b10bbbfc-cf9e-42e0-be17-e2c3e1d2600d","name":"The Beatles"}}],"genres":[{"id":"rock","name":"rock","count":26}],"tags":[{"name":"classic","count":5}],"rating":{"votes-count":70,"value":4.6},"releases":[{"id":"34e7ff03-8160-4d4f-a407-03f2c6510a2e","title":"Abbey Road","status":"Official","date":"1969-09-26","country":"GB","track-count":17,"barcode":"094638246824"}],"relations":[{"target-type":"url","type":"discogs","url":{"resource":"https://www.discogs.com/master/24047"}},{"target-type":"url","type":"wikidata","url":{"resource":"https://www.wikidata.org/wiki/Q173643"}},{"target-type":"url","type":"allmusic","url":{"resource":"https://www.allmusic.com/album/abbey-road-mw0000192938"}},{"target-type":"url","type":"purchase for download","url":{"resource":"https://thebeatles.bandcamp.com/album/abbey-road-remastered"}}]}`)
	record, err := NormalizeReleaseGroup(body, "observation", time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	if record.Titles[0].Value != "Abbey Road" || record.Classification.PrimaryType != "album" || record.Dates[0].Precision != "day" {
		t.Fatalf("record: %+v", record)
	}
	if len(record.ArtistCredits) != 1 || record.ArtistCredits[0].ArtistID != "b10bbbfc-cf9e-42e0-be17-e2c3e1d2600d" {
		t.Fatalf("credits: %+v", record.ArtistCredits)
	}
	if len(record.Editions) != 1 || record.Editions[0].Namespace != "release" || record.Editions[0].Country != "GB" {
		t.Fatalf("editions: %+v", record.Editions)
	}
	identities := map[string]string{}
	for _, candidate := range record.IdentityCandidates {
		identities[candidate.Provider] = candidate.NormalizedValue
	}
	for provider, want := range map[string]string{"discogs": "24047", "wikidata": "Q173643", "allmusic": "mw0000192938", "bandcamp": "thebeatles/abbey-road-remastered"} {
		if identities[provider] != want {
			t.Fatalf("%s identity: %+v", provider, identities)
		}
	}
}

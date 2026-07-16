package musicbrainz

import (
	"testing"
	"time"
)

func TestNormalizeArtistPreservesIdentityLocalesAndRelationships(t *testing.T) {
	t.Parallel()
	body := []byte(`{
		"id":"b10bbbfc-cf9e-42e0-be17-e2c3e1d2600d","name":"The Beatles","sort-name":"Beatles, The",
		"disambiguation":"UK rock band","type":"Group","country":"GB",
		"life-span":{"begin":"1960-03-27","end":"1970-04-10","ended":true},
		"area":{"id":"gb","name":"United Kingdom","iso-3166-1-codes":["GB"]},
		"begin-area":{"id":"liv","name":"Liverpool","iso-3166-2-codes":["GB-LIV"]},
		"isnis":["0000 0001 2170 7484"],
		"aliases":[{"name":"ザ・ビートルズ","sort-name":"ビートルズ（ザ）","locale":"ja","type":"Artist name","primary":false}],
		"genres":[{"id":"rock","name":"rock","count":47}],"tags":[{"name":"british","count":22}],
		"relations":[
			{"target-type":"url","type":"discogs","url":{"resource":"https://www.discogs.com/artist/82730"}},
			{"target-type":"url","type":"streaming","url":{"resource":"https://music.apple.com/gb/artist/the-beatles/136975"}},
			{"target-type":"url","type":"free streaming","url":{"resource":"https://www.deezer.com/artist/1"}},
			{"target-type":"url","type":"wikidata","url":{"resource":"https://www.wikidata.org/wiki/Q1299"}},
			{"target-type":"url","type":"free streaming","url":{"resource":"https://tidal.com/browse/artist/3529"}},
			{"target-type":"url","type":"purchase for download","url":{"resource":"https://thebeatles.bandcamp.com/"}},
			{"target-type":"url","type":"review","url":{"resource":"https://daily.bandcamp.com/features/the-beatles"}},
			{"target-type":"artist","type":"member of band","direction":"backward","artist":{"id":"42a8f507-8412-4611-854f-926571049fa0","name":"John Lennon"}}
		]
	}`)
	record, err := NormalizeArtist(body, "observation", time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	if record.Names[0].Value != "The Beatles" || record.Names[2].Language != "ja" {
		t.Fatalf("names: %+v", record.Names)
	}
	if len(record.Lifecycle.Dates) != 2 || record.Lifecycle.Dates[0].Precision != "day" || len(record.Areas) != 2 {
		t.Fatalf("lifecycle/areas: %+v / %+v", record.Lifecycle, record.Areas)
	}
	identities := map[string]string{}
	for _, candidate := range record.IdentityCandidates {
		identities[candidate.Provider] = candidate.NormalizedValue
	}
	for provider, expected := range map[string]string{"discogs": "82730", "apple": "136975", "deezer": "1", "wikidata": "Q1299", "isni": "0000000121707484", "tidal": "3529", "bandcamp": "thebeatles"} {
		if identities[provider] != expected {
			t.Fatalf("identity %s: got %q, all=%+v", provider, identities[provider], identities)
		}
	}
	// daily.bandcamp.com is Bandcamp's own editorial site, never artist identity.
	if len(record.IdentityCandidates) != 8 {
		t.Fatalf("identity candidates: %+v", record.IdentityCandidates)
	}
	if len(record.Relationships) != 1 || record.Relationships[0].TargetName != "John Lennon" {
		t.Fatalf("relationships: %+v", record.Relationships)
	}
}

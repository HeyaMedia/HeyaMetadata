package wikidata

import (
	"testing"
	"time"
)

func TestNormalizeReleaseGroupUnlocksCatalogAlbums(t *testing.T) {
	body := []byte(`{"entities":{"Q173643":{"id":"Q173643","labels":{"en":{"language":"en","value":"Abbey Road"}},"aliases":{"de":[{"language":"de","value":"Abbey Road Album"}]},"descriptions":{"en":{"language":"en","value":"1969 studio album"}},"claims":{"P436":[{"rank":"normal","mainsnak":{"snaktype":"value","datavalue":{"type":"string","value":"9162580e-5df4-32de-80cc-f45a8d8a9b1d"}}}],"P1954":[{"rank":"normal","mainsnak":{"snaktype":"value","datavalue":{"type":"string","value":"24047"}}}],"P2205":[{"rank":"normal","mainsnak":{"snaktype":"value","datavalue":{"type":"string","value":"0ETFjACtuP2ADo6LFhL6HN"}}}],"P2281":[{"rank":"normal","mainsnak":{"snaktype":"value","datavalue":{"type":"string","value":"1441164426"}}}],"P2723":[{"rank":"normal","mainsnak":{"snaktype":"value","datavalue":{"type":"string","value":"12047952"}}}],"P577":[{"rank":"normal","mainsnak":{"snaktype":"value","datavalue":{"type":"time","value":{"time":"+1969-09-26T00:00:00Z","precision":11}}}}],"P18":[{"rank":"normal","mainsnak":{"snaktype":"value","datavalue":{"type":"string","value":"Abbey Road.jpg"}}}],"P175":[{"rank":"normal","mainsnak":{"snaktype":"value","datavalue":{"type":"wikibase-entityid","value":{"id":"Q1299"}}}}]}}}}`)
	record, err := NormalizeReleaseGroup(body, "Q173643", "observation", time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	identities := map[string]string{}
	for _, candidate := range record.IdentityCandidates {
		identities[candidate.Provider] = candidate.NormalizedValue
	}
	for provider, want := range map[string]string{"musicbrainz": "9162580e-5df4-32de-80cc-f45a8d8a9b1d", "discogs": "24047", "spotify": "0ETFjACtuP2ADo6LFhL6HN", "apple": "1441164426", "deezer": "12047952"} {
		if identities[provider] != want {
			t.Fatalf("%s identity: %+v", provider, identities)
		}
	}
	if record.Dates[0].Value != "1969-09-26" || len(record.Images) != 1 || record.ArtistCredits[0].ArtistID != "Q1299" {
		t.Fatalf("record: %+v", record)
	}
}

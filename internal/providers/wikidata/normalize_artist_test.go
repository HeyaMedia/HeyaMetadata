package wikidata

import (
	"testing"
	"time"
)

func TestNormalizeArtistExtractsAuthorityIDsLanguagesAndLifecycle(t *testing.T) {
	body := []byte(`{"entities":{"Q1299":{"id":"Q1299","labels":{"en":{"language":"en","value":"The Beatles"},"ja":{"language":"ja","value":"ビートルズ"}},"descriptions":{"en":{"language":"en","value":"English rock band"}},"aliases":{"en":[{"language":"en","value":"Beatles"}]},"claims":{"P434":[{"rank":"normal","mainsnak":{"snaktype":"value","datavalue":{"type":"string","value":"b10bbbfc-cf9e-42e0-be17-e2c3e1d2600d"}}}],"P1953":[{"rank":"normal","mainsnak":{"snaktype":"value","datavalue":{"type":"string","value":"82730"}}}],"P571":[{"rank":"normal","mainsnak":{"snaktype":"value","datavalue":{"type":"time","value":{"time":"+1960-01-01T00:00:00Z","precision":9}}}}],"P18":[{"rank":"normal","mainsnak":{"snaktype":"value","datavalue":{"type":"string","value":"The Beatles members at New York City in 1964.jpg"}}}],"P740":[{"rank":"normal","mainsnak":{"snaktype":"value","datavalue":{"type":"wikibase-entityid","value":{"id":"Q24826"}}}}]}}}}`)
	record, err := NormalizeArtist(body, "Q1299", "observation", time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	if len(record.Names) != 2 || len(record.IdentityCandidates) != 0 || record.Lifecycle.Dates[0].Value != "1960" || len(record.Images) != 1 || record.Relationships[0].TargetID != "Q24826" {
		t.Fatalf("record: %+v", record)
	}
	for _, name := range record.Names {
		if name.Value == "Beatles" {
			t.Fatalf("unscoped Wikidata alias leaked into artist names: %+v", record.Names)
		}
	}
}

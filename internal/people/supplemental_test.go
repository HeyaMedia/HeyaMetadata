package people

import (
	"encoding/json"
	"testing"
)

func TestSupplementalHelpersRejectAmbiguousValues(t *testing.T) {
	t.Parallel()
	if value := linkedID("https://api.tvmaze.com/shows/82"); value != "82" {
		t.Fatalf("linked ID = %q", value)
	}
	if value := linkedID("https://api.tvmaze.com/shows/not-an-id"); value != "" {
		t.Fatalf("invalid linked ID = %q", value)
	}
	if value := validDate("1964-09-02"); value != "1964-09-02" {
		t.Fatalf("valid date = %q", value)
	}
	if value := validDate("1964"); value != "" {
		t.Fatalf("partial date = %q", value)
	}
}

func TestTVDBPersonCharacterUsesTopLevelMediaIDs(t *testing.T) {
	t.Parallel()
	var value tvdbPersonEnvelope
	err := json.Unmarshal([]byte(`{"data":{"id":247873,"gender":1,"name":"Aidan Gillen","characters":[{"id":12136535,"name":"John Reid","peopleType":"Actor","movieId":2,"movie":{"name":"Bohemian Rhapsody","year":"2018"}},{"id":13000000,"name":"Aberama Gold","peopleType":"Actor","seriesId":270915,"series":{"name":"Peaky Blinders","year":"2013"}}]}}`), &value)
	if err != nil {
		t.Fatal(err)
	}
	if value.Data.Gender != 1 || len(value.Data.Characters) != 2 || value.Data.Characters[0].MovieID != 2 || value.Data.Characters[1].SeriesID != 270915 {
		t.Fatalf("decoded TVDB person: %+v", value.Data)
	}
}

func TestTVDBPersonRemoteIDsMapToCanonicalEvidence(t *testing.T) {
	t.Parallel()
	tests := []struct {
		id         string
		remoteType int
		source     string
		provider   string
		namespace  string
		value      string
	}{
		{"nm0165087", 16, "IMDB", "imdb", "name", "nm0165087"},
		{"Q216160", 0, "Wikidata", "wikidata", "item", "Q216160"},
		{"JeremyClarkson", 5, "Facebook", "facebook", "person", "JeremyClarkson"},
	}
	for _, test := range tests {
		claim, ok := tvdbPersonRemoteID(test.id, test.remoteType, test.source)
		if !ok || claim.Provider != test.provider || claim.Namespace != test.namespace || claim.Value != test.value {
			t.Fatalf("remote ID %#v mapped to %#v, ok=%v", test, claim, ok)
		}
	}
	if _, ok := tvdbPersonRemoteID("value", 999, "Unknown"); ok {
		t.Fatal("unknown TVDB remote ID was accepted")
	}
}

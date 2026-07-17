package tvdb

import (
	"testing"
	"time"
)

func TestNormalizeMoviePreservesClaimsTranslationsCreditsAndArtwork(t *testing.T) {
	t.Parallel()
	body := []byte(`{"status":"success","data":{
		"id":123,"name":"The Matrix","runtime":136,"score":99.5,"originalLanguage":"eng",
		"status":{"name":"Released"},
		"aliases":[{"language":"deu","name":"Matrix"}],
		"translations":{"nameTranslations":[{"language":"dan","name":"The Matrix","isPrimary":true}],"overviewTranslations":[{"language":"eng","overview":"A hacker discovers reality."}]},
		"genres":[{"id":1,"name":"Science Fiction"}],"tagOptions":[{"id":2,"name":"Cyberpunk","tagName":"Theme"}],
		"remoteIds":[{"id":"tt0133093","type":2,"sourceName":"IMDB"},{"id":"603-the-matrix","type":12,"sourceName":"TheMovieDB.com"},{"id":"Q83495","type":18,"sourceName":"Wikidata"}],
		"releases":[{"country":"usa","date":"1999-03-31","detail":"Theatrical"}],
		"contentRatings":[{"country":"usa","name":"R"}],
		"characters":[{"id":7,"peopleId":42,"personName":"Keanu Reeves","peopleType":"Actor","name":"Neo","sort":1,"personImgURL":"/people/keanu.jpg"}],
		"companies":{"studio":[{"id":9,"name":"Warner Bros."}]},
		"artworks":[{"id":88,"image":"/movies/poster.jpg","type":14,"language":"eng","width":1000,"height":1500,"score":8.5}]
	}}`)
	record, err := Normalize(body, "primary", []string{"search"}, time.Unix(1, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	if record.ProviderRecord.Value != "123" || len(record.IdentityCandidates) != 4 {
		t.Fatalf("identity: %+v", record.IdentityCandidates)
	}
	if record.ProviderRecord.NormalizerVersion != "tvdb-movie/v2" || record.IdentityCandidates[2].Provider != "tmdb" || record.IdentityCandidates[2].NormalizedValue != "603" {
		t.Fatalf("TMDB remote identity: version=%s candidates=%+v", record.ProviderRecord.NormalizerVersion, record.IdentityCandidates)
	}
	if record.Measurements.RuntimeMinutes == nil || *record.Measurements.RuntimeMinutes != 136 || len(record.Descriptions) != 1 {
		t.Fatalf("measurements/descriptions: %+v", record)
	}
	if len(record.Credits) != 1 || record.Credits[0].Character != "Neo" || len(record.Images) != 1 || record.Images[0].Class != "poster" {
		t.Fatalf("credits/artwork: %+v / %+v", record.Credits, record.Images)
	}
	if len(record.Lifecycle.ReleaseEvents) != 1 || record.Lifecycle.ReleaseEvents[0].Certification != "R" {
		t.Fatalf("release: %+v", record.Lifecycle.ReleaseEvents)
	}
}

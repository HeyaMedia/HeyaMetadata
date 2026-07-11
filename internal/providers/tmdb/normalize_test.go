package tmdb

import (
	"testing"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/config"
)

func TestNormalizeMoviePreservesIdentityLocaleAndClassification(t *testing.T) {
	t.Parallel()
	body := []byte(`{
      "id":129,"title":"Spirited Away","original_title":"千と千尋の神隠し",
      "original_language":"ja","overview":"A girl enters a spirit world.",
      "release_date":"2001-07-20","status":"Released","runtime":125,
      "genres":[{"id":16,"name":"Animation"}],
      "external_ids":{"imdb_id":"tt0245429","wikidata_id":"Q155653"},
      "translations":{"translations":[{"iso_639_1":"da","iso_3166_1":"DK","data":{"title":"Chihiro og heksene","overview":"Dansk beskrivelse"}}]},
      "release_dates":{"results":[{"iso_3166_1":"JP","release_dates":[{"certification":"G","release_date":"2001-07-20T00:00:00.000Z","type":3}]}]},
      "images":{"posters":[{"file_path":"/poster.jpg","width":1000,"height":1500,"iso_639_1":"ja","vote_average":8.2,"vote_count":4}]}
    }`)
	record, err := Normalize(body, nil, "00000000-0000-0000-0000-000000000001", nil, time.Unix(1, 0).UTC(), "en-US")
	if err != nil {
		t.Fatal(err)
	}
	if record.ProviderRecord.Value != "129" || !record.Classification.AnimationEvidence || record.Classification.OriginalLanguage != "ja" {
		t.Fatalf("unexpected normalized record: %+v", record)
	}
	if len(record.IdentityCandidates) != 3 {
		t.Fatalf("identity candidates: got %d", len(record.IdentityCandidates))
	}
	foundDanish := false
	for _, title := range record.Titles {
		if title.Language == "da" && title.Value == "Chihiro og heksene" {
			foundDanish = true
		}
	}
	if !foundDanish {
		t.Fatal("localized Danish title was not preserved")
	}
	if len(record.Images) != 1 || record.Images[0].SourceURL != "https://image.tmdb.org/t/p/original/poster.jpg" {
		t.Fatalf("images: %+v", record.Images)
	}
}

func TestCapabilityUsesFortyEightHourLifecyclePrefix(t *testing.T) {
	t.Parallel()
	policy := New(config.TMDBConfig{}).Capability().RawRetention
	if policy.Duration != 48*time.Hour || policy.ObjectPrefix != "ephemeral/48h" || policy.Class != "provider_raw_48h" {
		t.Fatalf("retention policy: %+v", policy)
	}
}

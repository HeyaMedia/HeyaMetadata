package images

import (
	"reflect"
	"testing"
)

func TestLanguagePreferencesNormalizeISO639AndRespectOrder(t *testing.T) {
	t.Parallel()
	got := LanguagePreferences("en-GB", "dan,ja", "de-DE;q=0.9, en;q=0.8")
	want := []string{"en-GB", "da", "ja", "de-DE", "en"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("preferences: got %v want %v", got, want)
	}
}

func TestLanguageAlwaysOutranksPopularityAndResolution(t *testing.T) {
	t.Parallel()
	candidates := []EntityImageCandidate{
		{ID: "russian", Class: "poster", Language: "ru", Width: 4000, Height: 6000, ProviderScore: 100},
		{ID: "neutral", Class: "poster", Width: 3000, Height: 4500, ProviderScore: 50},
		{ID: "english", Class: "poster", Language: "eng", Width: 500, Height: 750, ProviderScore: 1},
	}
	ranked := RankCandidates(candidates, []string{"en-US"}, "")
	if ranked[0].ID != "english" || !ranked[0].Selected || ranked[0].SelectionReason != "base_language" {
		t.Fatalf("selected %+v", ranked[0])
	}
	if ranked[1].ID != "neutral" || ranked[1].SelectionReason != "neutral" {
		t.Fatalf("neutral fallback %+v", ranked[1])
	}
}

func TestSelectionIsIndependentPerArtworkClass(t *testing.T) {
	t.Parallel()
	ranked := RankCandidates([]EntityImageCandidate{
		{ID: "ru-poster", Class: "poster", Language: "ru"},
		{ID: "en-poster", Class: "poster", Language: "en"},
		{ID: "neutral-logo", Class: "logo"},
		{ID: "ja-logo", Class: "logo", Language: "ja"},
	}, []string{"en"}, "")
	selected := map[string]string{}
	for _, candidate := range ranked {
		if candidate.Selected {
			selected[candidate.Class] = candidate.ID
		}
	}
	want := map[string]string{"logo": "neutral-logo", "poster": "en-poster"}
	if !reflect.DeepEqual(selected, want) {
		t.Fatalf("selected: got %v want %v", selected, want)
	}
}

func TestNeutralArtworkWinsWithoutLanguagePreferences(t *testing.T) {
	t.Parallel()
	ranked := RankCandidates([]EntityImageCandidate{
		{ID: "german", Class: "backdrop", Language: "de", ProviderScore: 10},
		{ID: "neutral", Class: "backdrop", ProviderScore: 1},
	}, nil, "")
	if ranked[0].ID != "neutral" || !ranked[0].Selected {
		t.Fatalf("selected %+v", ranked[0])
	}
}

func TestDifferentWritingSystemsDoNotCountAsLanguageMatch(t *testing.T) {
	t.Parallel()
	ranked := RankCandidates([]EntityImageCandidate{
		{ID: "simplified", Class: "poster", Language: "zh-Hans", ProviderScore: 100},
		{ID: "neutral", Class: "poster"},
		{ID: "traditional", Class: "poster", Language: "zh-Hant", ProviderScore: 1},
	}, []string{"zh-Hant"}, "")
	if ranked[0].ID != "traditional" || ranked[1].ID != "neutral" || ranked[2].ID != "simplified" {
		t.Fatalf("script-aware order: %+v", ranked)
	}
}

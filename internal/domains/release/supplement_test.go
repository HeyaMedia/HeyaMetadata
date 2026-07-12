package release

import "testing"

func TestCompatibleRequiresBarcodeLayoutAndEditionTitle(t *testing.T) {
	spine := NormalizedRecord{Title: "残夢", Barcode: "6024-68033400", Date: "2024-07-09", Media: []Medium{{Tracks: make([]Track, 16)}}}
	candidate := NormalizedRecord{Title: "Zanmu", Barcode: "0602468033400", Date: "2024", Media: []Medium{{Tracks: make([]Track, 16)}}}
	if !Compatible(spine, candidate) {
		t.Fatal("expected romanized exact-barcode edition to match")
	}
	candidate.Title = "Zanmu (Remastered)"
	if !Compatible(spine, candidate) {
		t.Fatal("exact barcode, year, and layout should verify provider edition metadata even when annotation differs")
	}
	candidate.Media[0].Tracks = candidate.Media[0].Tracks[:15]
	if Compatible(spine, candidate) {
		t.Fatal("track layout mismatch must reject")
	}
}

func TestMatchTrackPrefersISRCThenVerifiedLayout(t *testing.T) {
	spine := Track{Sequence: 2, Title: "Show", DurationMS: 200000, Recording: Recording{ISRCs: []string{"JPAAA2400001"}}}
	candidate := NormalizedRecord{Media: []Medium{{Position: 1, Tracks: []Track{{ProviderID: "wrong", Sequence: 1, Title: "Other", Recording: Recording{ISRCs: []string{"JPAAA2400001"}}}}}}}
	if got := MatchTrack(spine, candidate, 1); got == nil || got.ProviderID != "wrong" {
		t.Fatalf("ISRC match: %+v", got)
	}
	candidate.Media[0].Tracks[0] = Track{ProviderID: "layout", Sequence: 2, Title: "Show", DurationMS: 201000}
	if got := MatchTrack(spine, candidate, 1); got == nil || got.ProviderID != "layout" {
		t.Fatalf("layout match: %+v", got)
	}
}
func TestCompatibleCatalogRequiresArtistYearAndStrongTrackCoverage(t *testing.T) {
	tracks := func(provider string) []Track {
		out := []Track{}
		for i, title := range []string{"Show", "DIGNITY", "向日葵", "唱"} {
			out = append(out, Track{ProviderID: provider, Sequence: i + 1, Title: title, DurationMS: 200000})
		}
		return out
	}
	spine := NormalizedRecord{Title: "残夢", Date: "2024-07-10", ArtistCredits: []ArtistCredit{{Name: "Ado"}}, Media: []Medium{{Position: 1, Tracks: tracks("mb")}}}
	apple := NormalizedRecord{Title: "Zanmu", Date: "2024-07-10", ArtistCredits: []ArtistCredit{{Name: "Ado"}}, Media: []Medium{{Position: 1, Tracks: tracks("apple")}}}
	if !CompatibleCatalog(spine, apple) {
		t.Fatal("expected verified romanized iTunes catalog match")
	}
	apple.ArtistCredits[0].Name = "Another Artist"
	if CompatibleCatalog(spine, apple) {
		t.Fatal("artist mismatch must reject")
	}
}

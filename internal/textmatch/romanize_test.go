package textmatch

import "testing"

func TestJapaneseCompoundAndUniversalRomanization(t *testing.T) {
	if got := normalized(Romanize("初夏")); got != "shoka" {
		t.Fatalf("初夏: %q", got)
	}
	if got := normalized(Romanize("ショカ")); got != "shoka" {
		t.Fatalf("ショカ: %q", got)
	}
	if got := normalized(Romanize("Николай")); got != "nikolai" {
		t.Fatalf("Cyrillic: %q", got)
	}
}
func TestReleaseKeysCollapseScriptAndCommercialEditionButNotRemix(t *testing.T) {
	if !EquivalentRelease("初夏", 2024, "Shoka (Deluxe Edition)", 2024) {
		t.Fatal("cross-script edition did not collapse")
	}
	if EquivalentRelease("Shoka", 2024, "Shoka (Remix)", 2024) {
		t.Fatal("remix must remain distinct")
	}
	if EquivalentRelease("Shoka", 2023, "Shoka", 2024) {
		t.Fatal("different years must remain distinct")
	}
	if !EquivalentRelease("北京", 2024, "Bei Jing", 2024) {
		t.Fatal("direct Han transliteration did not produce a comparison key")
	}
}

func TestReleaseKeysNormalizeUnicodeComposition(t *testing.T) {
	if !EquivalentRelease("この世界に二人だけ", 2025, "この世界に二人だけ", 2025) {
		t.Fatal("canonically equivalent Unicode titles did not match")
	}
}

package musiccredits

import (
	"strings"
	"testing"
)

func TestSplitFallbackUnderstandsCollaborationNotation(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		credit string
		want   []string
	}{
		{"Yoshiko & Alee", []string{"Yoshiko", "Alee"}},
		{"Ado feat. Hatsune Miku", []string{"Ado", "Hatsune Miku"}},
		{"Ado ft Hatsune Miku", []string{"Ado", "Hatsune Miku"}},
		{"Ado featuring Hatsune Miku", []string{"Ado", "Hatsune Miku"}},
		{"Ado f/ Hatsune Miku", []string{"Ado", "Hatsune Miku"}},
		{"Ado w/ Hatsune Miku", []string{"Ado", "Hatsune Miku"}},
		{"Ado presents Hatsune Miku", []string{"Ado", "Hatsune Miku"}},
		{"Ado meets Hatsune Miku", []string{"Ado", "Hatsune Miku"}},
		{"Ado; ano", []string{"Ado", "ano"}},
		{"Ado : ano", []string{"Ado", "ano"}},
		{"Ado x ano", []string{"Ado", "ano"}},
		{"Ado × ano", []string{"Ado", "ano"}},
		{"Ado / ano", []string{"Ado", "ano"}},
		{"Ado (feat. ano)", []string{"Ado", "ano"}},
	} {
		test := test
		t.Run(test.credit, func(t *testing.T) {
			t.Parallel()
			got := SplitFallback(test.credit)
			if strings.Join(got, "|") != strings.Join(test.want, "|") {
				t.Fatalf("SplitFallback(%q) = %#v, want %#v", test.credit, got, test.want)
			}
		})
	}
}

func TestContainsNameTriesLiteralCreditBeforeFallback(t *testing.T) {
	t.Parallel()
	equivalent := func(left, right string) bool {
		return strings.EqualFold(strings.TrimSpace(left), strings.TrimSpace(right))
	}
	if !ContainsName("Earth, Wind & Fire", []string{"Earth, Wind & Fire"}, equivalent) {
		t.Fatal("literal band name should match")
	}
	if !ContainsName("Yoshiko & Alee", []string{"Yoshiko"}, equivalent) {
		t.Fatal("collaboration member should match as fallback")
	}
	if ContainsName("Yoshiko & Alee", []string{"Radiohead"}, equivalent) {
		t.Fatal("unrelated artist matched")
	}
}

func TestSplitFallbackKeepsAmbiguousPunctuation(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"AC/DC", "Tyler, the Creator"} {
		if got := SplitFallback(value); len(got) != 1 || got[0] != value {
			t.Fatalf("SplitFallback(%q) = %#v", value, got)
		}
	}
}

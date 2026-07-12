package episodic

import "testing"

func TestSlugPreservesNonLatinCanonicalTitles(t *testing.T) {
	if got := Slug("葬送のフリーレン", 2023, "anime"); got != "葬送のフリーレン-2023" {
		t.Fatalf("slug: %q", got)
	}
}

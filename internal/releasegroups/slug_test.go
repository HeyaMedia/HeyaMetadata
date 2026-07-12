package releasegroups

import "testing"

func TestReleaseGroupSlugPreservesNonLatinTitles(t *testing.T) {
	if got := releaseGroupSlug("残夢", 2024); got != "残夢-2024" {
		t.Fatalf("release-group slug: %q", got)
	}
}

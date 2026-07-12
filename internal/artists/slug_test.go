package artists

import "testing"

func TestArtistSlugPreservesNonLatinNames(t *testing.T) {
	if got := artistSlug("ハク。"); got != "ハク" {
		t.Fatalf("artist slug: %q", got)
	}
}

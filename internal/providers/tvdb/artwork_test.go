package tvdb

import "testing"

func TestArtworkClassCoversTVDBRegistry(t *testing.T) {
	t.Parallel()
	want := map[int]string{
		1: "banner", 2: "poster", 3: "backdrop", 5: "icon",
		6: "banner", 7: "poster", 8: "backdrop", 10: "icon",
		11: "still", 12: "still", 13: "profile", 14: "poster",
		15: "backdrop", 16: "banner", 18: "icon", 19: "icon",
		20: "cinemagraph", 21: "cinemagraph", 22: "clearart",
		23: "clearlogo", 24: "clearart", 25: "clearlogo",
		26: "icon", 27: "poster",
	}
	for id, class := range want {
		if got := ArtworkClass(id); got != class {
			t.Errorf("ArtworkClass(%d) = %q, want %q", id, got, class)
		}
	}
	if got := ArtworkClass(999); got != "" {
		t.Fatalf("unknown artwork class = %q", got)
	}
}

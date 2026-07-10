package coverage

import "testing"

func TestMovieCatalogIsValid(t *testing.T) {
	t.Parallel()

	catalog, err := Movie()
	if err != nil {
		t.Fatal(err)
	}
	if got, wantMinimum := len(catalog.Entries), 30; got < wantMinimum {
		t.Fatalf("movie coverage entries: got %d, want at least %d", got, wantMinimum)
	}
}

func TestMovieCatalogRetainsLegacyProviderFloor(t *testing.T) {
	t.Parallel()

	catalog, err := Movie()
	if err != nil {
		t.Fatal(err)
	}

	providers := map[string]bool{}
	for _, entry := range catalog.Entries {
		for _, provider := range entry.Providers {
			providers[provider] = true
		}
	}
	for _, provider := range []string{"tmdb", "tvdb", "omdb", "fanart"} {
		if !providers[provider] {
			t.Errorf("movie coverage does not include legacy provider %q", provider)
		}
	}
}

func TestMovieCatalogIDsAreUniqueAndSorted(t *testing.T) {
	t.Parallel()

	catalog, err := Movie()
	if err != nil {
		t.Fatal(err)
	}
	ids := catalog.IDs()
	for i := 1; i < len(ids); i++ {
		if ids[i-1] >= ids[i] {
			t.Fatalf("catalog IDs are not strictly sorted: %q then %q", ids[i-1], ids[i])
		}
	}
}

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

func TestMusicCatalogIsValidAndRetainsProviderFloor(t *testing.T) {
	t.Parallel()
	catalog, err := Music()
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Entries) < 16 {
		t.Fatalf("music coverage entries: %d", len(catalog.Entries))
	}
	providers := map[string]bool{}
	for _, entry := range catalog.Entries {
		for _, provider := range entry.Providers {
			providers[provider] = true
		}
	}
	for _, provider := range []string{"musicbrainz", "apple", "deezer", "discogs", "lastfm", "wikidata", "openopus"} {
		if !providers[provider] {
			t.Errorf("music coverage does not include provider %q", provider)
		}
	}
}

func TestBooksAndTVCatalogsAreValid(t *testing.T) {
	t.Parallel()
	books, err := Books()
	if err != nil {
		t.Fatal(err)
	}
	if len(books.Entries) < 8 {
		t.Fatalf("book coverage entries: %d", len(books.Entries))
	}
	tv, err := TV()
	if err != nil {
		t.Fatal(err)
	}
	if len(tv.Entries) < 4 {
		t.Fatalf("TV coverage entries: %d", len(tv.Entries))
	}
}

func TestPeopleCatalogIsValid(t *testing.T) {
	t.Parallel()
	catalog, err := People()
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Entries) < 6 {
		t.Fatalf("people coverage entries: %d", len(catalog.Entries))
	}
}

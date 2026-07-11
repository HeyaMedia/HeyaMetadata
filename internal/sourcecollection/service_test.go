package sourcecollection

import "testing"

func TestRegisteredProvidersAreStableAndUnique(t *testing.T) {
	t.Parallel()
	seen := map[string]bool{}
	for _, provider := range RegisteredProviders() {
		if provider == "" || seen[provider] {
			t.Fatalf("invalid provider registry: %+v", RegisteredProviders())
		}
		seen[provider] = true
	}
	for _, expected := range []string{"anidb", "apple", "deezer", "discogs", "lastfm", "musicbrainz", "openopus", "tvmaze", "wikidata"} {
		if !seen[expected] {
			t.Fatalf("provider %s is not registered", expected)
		}
	}
}

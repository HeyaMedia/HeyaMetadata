package episodic

import "testing"

func TestEpisodeIdentityKeyUsesDeterministicNumberingPriority(t *testing.T) {
	episode := Episode{ProviderID: "upstream-9", Numbers: []EpisodeNumber{{Scheme: "TVMaze", Season: 2, Number: 3.5}, {Scheme: "tmdb", Season: 2, Number: 4}}}
	if got, want := episodeIdentityKey(episode), "tmdb:2:4"; got != want {
		t.Fatalf("episodeIdentityKey() = %q, want %q", got, want)
	}
	episode.Numbers[0], episode.Numbers[1] = episode.Numbers[1], episode.Numbers[0]
	if got, want := episodeIdentityKey(episode), "tmdb:2:4"; got != want {
		t.Fatalf("episodeIdentityKey() after reordering = %q, want %q", got, want)
	}
}

func TestEpisodeIdentityKeyFallsBackToProviderID(t *testing.T) {
	if got, want := episodeIdentityKey(Episode{ProviderID: "abc"}), "provider:0:abc"; got != want {
		t.Fatalf("episodeIdentityKey() = %q, want %q", got, want)
	}
}

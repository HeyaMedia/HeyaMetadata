package episodic

import "testing"

func TestEpisodeIdentityKeyUsesPrimaryNumbering(t *testing.T) {
	episode := Episode{ProviderID: "upstream-9", Numbers: []EpisodeNumber{{Scheme: "TVMaze", Season: 2, Number: 3.5}, {Scheme: "tmdb", Season: 2, Number: 4}}}
	if got, want := episodeIdentityKey(episode), "tvmaze:2:3.5"; got != want {
		t.Fatalf("episodeIdentityKey() = %q, want %q", got, want)
	}
}

func TestEpisodeIdentityKeyFallsBackToProviderID(t *testing.T) {
	if got, want := episodeIdentityKey(Episode{ProviderID: "abc"}), "provider:0:abc"; got != want {
		t.Fatalf("episodeIdentityKey() = %q, want %q", got, want)
	}
}

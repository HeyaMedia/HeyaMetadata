package recordings

import (
	"testing"

	releasedomain "github.com/HeyaMedia/HeyaMetadata/internal/domains/release"
)

func TestMergeDataPreservesStandaloneEvidenceDuringReleaseRefresh(t *testing.T) {
	existing := releasedomain.Recording{Title: "Song", Genres: []releasedomain.WeightedTerm{{Name: "rock"}}, Releases: []releasedomain.RecordingRelease{{ProviderID: "release"}}, Links: []releasedomain.Link{{Type: "lyrics", URL: "https://example.com"}}}
	incoming := releasedomain.Recording{Title: "Song (credited)", DurationMS: 123000}
	merged := MergeData(existing, incoming)
	if merged.Title != incoming.Title || merged.DurationMS != incoming.DurationMS || len(merged.Genres) != 1 || len(merged.Releases) != 1 || len(merged.Links) != 1 {
		t.Fatalf("merged recording: %+v", merged)
	}
}

func TestMusicBrainzArtistCreditIDsAreValidatedAndDeduplicated(t *testing.T) {
	const artistID = "B10BBBFC-CF9E-42E0-BE17-E2C3E1D2600D"
	credits := []releasedomain.ArtistCredit{
		{ArtistProvider: "MusicBrainz", ArtistNamespace: "artist", ArtistID: artistID},
		{ArtistProvider: "musicbrainz", ArtistNamespace: "artist", ArtistID: artistID},
		{ArtistProvider: "musicbrainz", ArtistNamespace: "artist", ArtistID: "not-a-uuid"},
		{ArtistProvider: "discogs", ArtistNamespace: "artist", ArtistID: artistID},
	}
	got := musicBrainzArtistCreditIDs(credits)
	if len(got) != 1 || got[0] != "b10bbbfc-cf9e-42e0-be17-e2c3e1d2600d" {
		t.Fatalf("credit IDs = %#v", got)
	}
}

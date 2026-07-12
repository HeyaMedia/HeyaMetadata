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

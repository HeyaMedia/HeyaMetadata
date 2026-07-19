package musiccatalog

import (
	"testing"

	releasedomain "github.com/HeyaMedia/HeyaMetadata/internal/domains/release"
	rgdomain "github.com/HeyaMedia/HeyaMetadata/internal/domains/releasegroup"
)

func TestDirectArtistBridgeRequiresCompleteProviderRoots(t *testing.T) {
	valid := ArtistIdentityBridge{
		ArtistEntityID:             "canonical",
		MusicBrainzArtistID:        "artist-mbid",
		MusicBrainzReleaseGroupID:  "release-group-mbid",
		StorefrontProvider:         "apple",
		StorefrontArtistID:         "42",
		StorefrontReleaseNamespace: "album",
		StorefrontReleaseID:        "84",
	}
	if !validArtistIdentityBridge(valid) {
		t.Fatal("complete bridge was rejected")
	}
	valid.StorefrontReleaseNamespace = "artist"
	if validArtistIdentityBridge(valid) {
		t.Fatal("artist identifier was accepted as release evidence")
	}
	valid.StorefrontReleaseNamespace = "album"
	valid.StorefrontProvider = "lastfm"
	if validArtistIdentityBridge(valid) {
		t.Fatal("unsupported storefront was accepted")
	}
}

func TestDirectReleaseConceptRequiresTitleAndCompatibleYear(t *testing.T) {
	group := rgdomain.NormalizedRecordV1{
		Titles: []rgdomain.Title{{Value: "Rumours", Primary: true}},
		Dates:  []rgdomain.DateValue{{Value: "1977-02-04"}},
	}
	storefront := candidate{Title: "Rumours", Date: "1977-02-04"}
	if !directReleaseConceptCompatible(group, storefront) {
		t.Fatal("same release concept was rejected")
	}
	storefront.Date = "2017-02-04"
	if directReleaseConceptCompatible(group, storefront) {
		t.Fatal("different release year was accepted")
	}
	storefront.Date = "1977"
	storefront.Title = "Greatest Hits"
	if directReleaseConceptCompatible(group, storefront) {
		t.Fatal("different release title was accepted")
	}
}

func TestDirectBridgeEditionsBoundsAndPrioritizesEvidence(t *testing.T) {
	editions := []rgdomain.Edition{
		{ProviderID: "one", Date: rgdomain.DateValue{Value: "1976"}, TrackCount: 4},
		{ProviderID: "two", Date: rgdomain.DateValue{Value: "1977"}, TrackCount: 11},
		{ProviderID: "three", Date: rgdomain.DateValue{Value: "1977"}, TrackCount: 11, Barcode: "012345"},
		{ProviderID: "four"},
		{ProviderID: "five"},
		{ProviderID: "six"},
		{ProviderID: "seven"},
	}
	storefront := candidate{Date: "1977-02-04", Metadata: map[string]any{"track_count": 11}}
	got := directBridgeEditions(editions, storefront, detailEvidence{Barcode: "12345"})
	if len(got) != directArtistBridgeReleaseLimit {
		t.Fatalf("edition fetch set has %d entries, want %d", len(got), directArtistBridgeReleaseLimit)
	}
	if got[0].ProviderID != "three" || got[1].ProviderID != "two" {
		t.Fatalf("evidence-bearing editions were not prioritized: %+v", got)
	}
}

func TestMusicBrainzReleaseEvidencePreservesBarcodeISRCAndOrder(t *testing.T) {
	record := releasedomain.NormalizedRecord{
		Barcode: "0 12345",
		Media: []releasedomain.Medium{{Tracks: []releasedomain.Track{
			{Title: "First", DurationMS: 1000, Recording: releasedomain.Recording{ProviderID: "recording-one", ISRCs: []string{"us-abc-1"}}},
			{Title: "Second", DurationMS: 2000, Recording: releasedomain.Recording{ProviderID: "recording-two", ISRCs: []string{"US-ABC-2"}}},
		}}},
	}
	got := evidenceFromMusicBrainzRelease(record)
	if got.Barcode != "12345" || !got.ISRCs["US-ABC-1"] || !got.ISRCs["US-ABC-2"] {
		t.Fatalf("identity evidence was not normalized: %+v", got)
	}
	if len(got.Tracks) != 2 || got.Tracks[0].Title != "First" || len(got.Tracklists) != 1 {
		t.Fatalf("ordered track evidence was not preserved: %+v", got)
	}
}

func TestDirectReleaseEvidenceAcceptsCompleteAlbumTracklistDespiteMasteringDrift(t *testing.T) {
	left := detailEvidence{Tracks: []trackEvidence{
		{Title: "Second Hand News", DurationMS: 163000},
		{Title: "Dreams", DurationMS: 254000},
		{Title: "Never Going Back Again", DurationMS: 122000},
		{Title: "Don't Stop", DurationMS: 191000},
	}}
	right := detailEvidence{Tracks: []trackEvidence{
		{Title: "Second Hand News", DurationMS: 176307},
		{Title: "Dreams", DurationMS: 257800},
		{Title: "Never Going Back Again", DurationMS: 134400},
		{Title: "Don’t Stop", DurationMS: 193347},
	}}
	if !directReleaseEvidenceMatches(left, right) {
		t.Fatal("complete ordered album tracklist was rejected because mastering durations drifted")
	}
	right.Tracks[2].Title = "Unrelated Song"
	if directReleaseEvidenceMatches(left, right) {
		t.Fatal("partial ordered tracklist was accepted as exact release identity")
	}
	if directReleaseEvidenceMatches(
		detailEvidence{Tracks: left.Tracks[:1]},
		detailEvidence{Tracks: right.Tracks[:1]},
	) {
		t.Fatal("one-track title match was accepted without hard recording evidence")
	}
}

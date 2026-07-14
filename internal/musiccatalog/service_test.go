package musiccatalog

import "testing"

func TestArtistCatalogExcludedOnlyForSyntheticVariousArtists(t *testing.T) {
	if !artistCatalogExcluded(variousArtistsMusicBrainzID) {
		t.Fatal("MusicBrainz Various Artists must not build an unbounded catalog")
	}
	if artistCatalogExcluded("a74b1b7f-71a5-4011-9441-d0b5e4122711") {
		t.Fatal("real artist was excluded from catalog reconciliation")
	}
}

func TestDirectStorefrontRootRemainsAuthoritativeBesideMusicBrainz(t *testing.T) {
	t.Parallel()
	sets := map[string]map[string][]candidate{
		"musicbrainz": {"mb": {{Provider: "musicbrainz", Namespace: "release_group", ID: "mb-release", Title: "Old Album", Date: "2020", Kind: "album"}}},
		"apple": {"apple-root": {
			{Provider: "apple", Namespace: "album", ID: "old", Title: "Old Album", Date: "2020", Kind: "album", Metadata: map[string]any{}},
			{Provider: "apple", Namespace: "album", ID: "fresh", Title: "Fresh Single", Date: "2026", Kind: "single", Metadata: map[string]any{}},
		}},
	}
	directRoots := map[string]string{"apple": "apple-root"}
	selected := selectProviderIdentities(sets, []string{"Artist"}, directRoots)
	selected, dropped := gateSelectedStorefronts(selected, directRoots)
	if dropped != 0 || len(selected["apple"]) != 2 {
		t.Fatalf("direct Apple root was gated: dropped=%d selected=%#v", dropped, selected["apple"])
	}
	for _, value := range selected["apple"] {
		if value.Metadata["catalog_identity_gate"] != "canonical_artist_provider_root" {
			t.Fatalf("Apple source lacks direct-root gate: %#v", value.Metadata)
		}
	}
}

func TestUnclaimedStorefrontStillNeedsCatalogOverlap(t *testing.T) {
	t.Parallel()
	sets := map[string]map[string][]candidate{
		"musicbrainz": {"mb": {{Provider: "musicbrainz", ID: "mb-release", Title: "Old Album", Date: "2020", Kind: "album"}}},
		"apple":       {"namesake": {{Provider: "apple", ID: "fresh", Title: "Unrelated", Date: "2026", Kind: "single", ArtistName: "Artist"}}},
	}
	selected := selectProviderIdentities(sets, []string{"Artist"}, nil)
	if len(selected["apple"]) != 0 {
		t.Fatalf("unclaimed namesake Apple page was selected: %#v", selected["apple"])
	}
}

func TestCatalogArtistCompatibilityUnderstandsPresentationCredits(t *testing.T) {
	t.Parallel()
	if !catalogArtistCompatible([]candidate{{ArtistName: "Yoshiko feat. Alee"}}, []string{"Yoshiko"}) {
		t.Fatal("featured credit did not retain the primary artist catalog")
	}
	if !catalogArtistCompatible([]candidate{{ArtistName: "Earth, Wind & Fire"}}, []string{"Earth, Wind & Fire"}) {
		t.Fatal("literal ampersand band name did not match")
	}
	if catalogArtistCompatible([]candidate{{ArtistName: "Yoshiko & Alee"}}, []string{"Radiohead"}) {
		t.Fatal("unrelated collaborative credit matched")
	}
}

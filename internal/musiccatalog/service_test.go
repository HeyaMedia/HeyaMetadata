package musiccatalog

import "testing"

func TestClusterCandidatesCombinesScriptsAndProviderEditions(t *testing.T) {
	t.Parallel()
	values := []candidate{
		{Provider: "musicbrainz", Namespace: "release_group", ID: "mb", Title: "ハク", Date: "2022-10-12", Kind: "single"},
		{Provider: "apple", Namespace: "album", ID: "apple", Title: "Haku", Date: "2023-01-01", Kind: "single"},
		{Provider: "deezer", Namespace: "album", ID: "deezer", Title: "Haku (Deluxe Edition)", Date: "2023-01-01", Kind: "single"},
	}
	clusters := clusterCandidates(values)
	if len(clusters) != 1 {
		t.Fatalf("clusters: got %d, want 1", len(clusters))
	}
	if len(clusters[0].Sources) != 3 {
		t.Fatalf("sources: got %d, want 3", len(clusters[0].Sources))
	}
}

func TestClusterCandidatesDoesNotMergeDistinctMusicBrainzWorks(t *testing.T) {
	t.Parallel()
	values := []candidate{
		{Provider: "musicbrainz", ID: "old", Title: "Home", Date: "2001", Kind: "album"},
		{Provider: "musicbrainz", ID: "new", Title: "Home", Date: "2024", Kind: "album"},
	}
	if got := len(clusterCandidates(values)); got != 2 {
		t.Fatalf("clusters: got %d, want 2", got)
	}
}

func TestAmbiguousProviderIdentityRequiresUniqueCatalogOverlap(t *testing.T) {
	t.Parallel()
	sets := map[string]map[string][]candidate{
		"musicbrainz": {"mb": {{Provider: "musicbrainz", Title: "Zanmu", Date: "2024", Kind: "album"}}},
		"deezer": {
			"correct": {{Provider: "deezer", Title: "Zanmu", Date: "2024", Kind: "album", ArtistName: "Ado"}},
			"wrong":   {{Provider: "deezer", Title: "Unrelated", Date: "2024", Kind: "album", ArtistName: "Ado"}},
		},
	}
	selected := selectProviderIdentities(sets, []string{"Ado"})
	if len(selected["deezer"]) != 1 || selected["deezer"][0].Title != "Zanmu" {
		t.Fatalf("selected wrong provider identity: %#v", selected["deezer"])
	}
}

func TestSingleProviderIdentityStillRequiresIndependentOverlap(t *testing.T) {
	t.Parallel()
	sets := map[string]map[string][]candidate{
		"musicbrainz": {"mb": {{Provider: "musicbrainz", Title: "Monstersound", Date: "2000", Kind: "single"}}},
		"lastfm":      {"mb": {{Provider: "lastfm", Title: "Unrelated Balloon Album", ArtistName: "Balloon"}}},
	}
	selected := selectProviderIdentities(sets, []string{"Balloon"})
	if len(selected["lastfm"]) != 0 {
		t.Fatalf("unsubstantiated Last.fm identity was selected: %#v", selected["lastfm"])
	}
}

func TestCatalogWithoutRepeatedArtistNameCanUseIndependentOverlap(t *testing.T) {
	t.Parallel()
	sets := map[string]map[string][]candidate{
		"musicbrainz": {"mb": {{Provider: "musicbrainz", Title: "Zanmu", Date: "2024", Kind: "album"}}},
		"deezer":      {"id": {{Provider: "deezer", Title: "Zanmu", Date: "2024", Kind: "album"}}},
	}
	selected := selectProviderIdentities(sets, []string{"Ado"})
	if len(selected["deezer"]) != 1 {
		t.Fatalf("overlapping artist-scoped catalog was rejected: %#v", selected)
	}
}

func TestTwoUnanchoredStorefrontsCannotBootstrapArtistIdentity(t *testing.T) {
	t.Parallel()
	sets := map[string]map[string][]candidate{
		"musicbrainz": {"mb": {{Provider: "musicbrainz", Title: "Monstersound", Kind: "single"}}},
		"apple":       {"apple": {{Provider: "apple", Title: "Wrong Namesake Album", Kind: "album", ArtistName: "Balloon"}}},
		"deezer":      {"deezer": {{Provider: "deezer", Title: "Wrong Namesake Album", Kind: "album"}}},
	}
	selected := selectProviderIdentities(sets, []string{"Balloon"})
	if len(selected["apple"]) != 0 || len(selected["deezer"]) != 0 {
		t.Fatalf("unanchored storefronts bootstrapped each other: %#v", selected)
	}
}

func TestOnlyAnchoredClustersArePublicDiscography(t *testing.T) {
	t.Parallel()
	if anchoredCluster(cluster{Sources: []candidate{{Provider: "apple"}, {Provider: "deezer"}}}) {
		t.Fatal("storefront-only cluster became public")
	}
	if !anchoredCluster(cluster{Sources: []candidate{{Provider: "discogs"}, {Provider: "apple"}}}) {
		t.Fatal("Discogs-anchored cluster was not public")
	}
}

func TestLastFMCannotCreateDiscographyItem(t *testing.T) {
	t.Parallel()
	clusters := clusterCandidates([]candidate{
		{Provider: "musicbrainz", Title: "Known", Kind: "album"},
		{Provider: "lastfm", Title: "Same-name collision", Kind: "album"},
	})
	if len(clusters) != 1 || clusters[0].Sources[0].Title != "Known" {
		t.Fatalf("Last.fm created an authoritative cluster: %#v", clusters)
	}
}

func TestDuplicateProviderEvidenceAppearsOncePerCluster(t *testing.T) {
	t.Parallel()
	clusters := clusterCandidates([]candidate{
		{Provider: "musicbrainz", Namespace: "release_group", ID: "mb", Title: "踊", Kind: "single"},
		{Provider: "lastfm", Namespace: "release_group", ID: "last", Title: "踊"},
		{Provider: "lastfm", Namespace: "release_group", ID: "last", Title: "踊"},
	})
	if len(clusters) != 1 || len(clusters[0].Sources) != 2 {
		t.Fatalf("duplicate evidence remained: %#v", clusters)
	}
}

func TestDiscogsNumericArtistSuffixIsComparisonOnly(t *testing.T) {
	t.Parallel()
	if got := stripDiscogsSuffix("Ado (18)"); got != "Ado" {
		t.Fatalf("suffix: got %q", got)
	}
}

func TestStorefrontTitleRemovesITunesReleaseTypeSuffix(t *testing.T) {
	t.Parallel()
	title, kind := storefrontTitle("Fuhen - Single", "Album", 1)
	if title != "Fuhen" || kind != "single" {
		t.Fatalf("storefront title: %q/%q", title, kind)
	}
}

func TestJapaneseKanjiAndRomajiCanCluster(t *testing.T) {
	t.Parallel()
	clusters := clusterCandidates([]candidate{
		{Provider: "musicbrainz", Title: "普変", Date: "2022-10-12", Kind: "single"},
		{Provider: "apple", Title: "Fuhen", Date: "2022-10-12T07:00:00Z", Kind: "single"},
	})
	if len(clusters) != 1 {
		t.Fatal("expected Japanese and romaji release titles to cluster")
	}
}

func TestProviderOnlyConfidenceRequiresIndependentAuthorities(t *testing.T) {
	t.Parallel()
	single := cluster{Sources: []candidate{{Provider: "apple"}, {Provider: "lastfm"}}}
	if got := clusterConfidence(single); got >= .8 {
		t.Fatalf("single authoritative provider confidence: %v", got)
	}
	consensus := cluster{Sources: []candidate{{Provider: "apple"}, {Provider: "deezer"}, {Provider: "lastfm"}}}
	if got := clusterConfidence(consensus); got < .9 {
		t.Fatalf("independent consensus confidence: %v", got)
	}
}

func TestProviderFormatsNormalizeToReleaseGroupTypes(t *testing.T) {
	t.Parallel()
	for input, want := range map[string]string{
		"CD, Maxi, Ltd":       "single",
		`12", RSD, Ltd`:       "single",
		"2xBlu-ray, Ltd, Liv": "album",
		"CD":                  "",
	} {
		if got := normalizeKind(input); got != want {
			t.Errorf("normalizeKind(%q): got %q, want %q", input, got, want)
		}
	}
}

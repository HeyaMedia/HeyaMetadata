package musiccatalog

import (
	"fmt"
	"testing"
)

func TestClusterCandidatesCombinesScriptsAndProviderEditions(t *testing.T) {
	t.Parallel()
	values := []candidate{
		{Provider: "musicbrainz", Namespace: "release_group", ID: "mb", Title: "ハク", Date: "2022-10-12", Kind: "single"},
		{Provider: "apple", Namespace: "album", ID: "apple", Title: "Haku", Date: "2022-10-12", Kind: "single"},
		{Provider: "deezer", Namespace: "album", ID: "deezer", Title: "Haku (Deluxe Edition)", Date: "2022-10-12", Kind: "single"},
	}
	clusters := clusterCandidates(values)
	if len(clusters) != 1 {
		t.Fatalf("clusters: got %d, want 1", len(clusters))
	}
	if len(clusters[0].Sources) != 3 {
		t.Fatalf("sources: got %d, want 3", len(clusters[0].Sources))
	}
}

func TestCandidateMatchRejectsSameTitleFromDifferentKnownYears(t *testing.T) {
	t.Parallel()
	left := candidate{Provider: "musicbrainz", Title: "Hibana", Date: "2026-03-25", Kind: "album"}
	right := candidate{Provider: "apple", Title: "Hibana", Date: "2025-04-25", Kind: "album"}
	if ok, _, _ := candidateMatch(left, right); ok {
		t.Fatal("different-year releases were merged by title")
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

func TestClusterCandidatesDoesNotMergeDistinctSameYearMusicBrainzGroups(t *testing.T) {
	t.Parallel()
	values := []candidate{
		{Provider: "musicbrainz", Namespace: "release_group", ID: "first", Title: "Home", Date: "2024", Kind: "album"},
		{Provider: "musicbrainz", Namespace: "release_group", ID: "second", Title: "Home", Date: "2024", Kind: "album"},
	}
	if got := len(clusterCandidates(values)); got != 2 {
		t.Fatalf("clusters: got %d, want 2", got)
	}
}

func TestAmbiguousSupplementDoesNotAttachToFirstCanonicalGroup(t *testing.T) {
	t.Parallel()
	clusters := clusterCandidates([]candidate{
		{Provider: "musicbrainz", Namespace: "release_group", ID: "first", Title: "Home", Date: "2024", Kind: "album"},
		{Provider: "musicbrainz", Namespace: "release_group", ID: "second", Title: "Home", Date: "2024", Kind: "album"},
		{Provider: "apple", Namespace: "album", ID: "apple", Title: "Home", Date: "2024", Kind: "album"},
	})
	if len(clusters) != 3 {
		t.Fatalf("ambiguous supplement attached arbitrarily: %#v", clusters)
	}
}

func TestDiscogsMasterCanAbsorbItsExplicitMainRelease(t *testing.T) {
	t.Parallel()
	clusters := clusterCandidates([]candidate{
		{Provider: "discogs", Namespace: "master", ID: "10", Title: "Album", Metadata: map[string]any{"main_release": 20}},
		{Provider: "discogs", Namespace: "release", ID: "20", Title: "Album"},
	})
	if len(clusters) != 1 || len(clusters[0].Sources) != 2 {
		t.Fatalf("explicit Discogs relation was lost: %#v", clusters)
	}
}

func TestClustersResolvingToSameCanonicalTargetEmitOnce(t *testing.T) {
	t.Parallel()
	clusters := coalesceTargetedClusters([]cluster{
		{TargetID: "canonical", PromotionState: "musicbrainz_spine", Sources: []candidate{{Provider: "musicbrainz", ID: "mb", Title: "Monstersound"}}},
		{TargetID: "canonical", PromotionState: "canonical", Sources: []candidate{{Provider: "discogs", ID: "discogs", Title: "Monstersound"}}},
	})
	if len(clusters) != 1 {
		t.Fatalf("clusters: got %d, want 1", len(clusters))
	}
	if len(clusters[0].Sources) != 2 || clusters[0].BridgeReason != "canonical_target" {
		t.Fatalf("canonical evidence was not combined: %#v", clusters[0])
	}
}

func TestIssuedTrackOverlapCoalescesRegionalReleaseGroups(t *testing.T) {
	t.Parallel()
	left := cluster{TargetID: "german", Sources: []candidate{{Provider: "musicbrainz", ID: "de", Title: "Monstersound", Date: "2000-12-12", Kind: "single"}}}
	right := cluster{TargetID: "danish", Sources: []candidate{{Provider: "musicbrainz", ID: "dk", Title: "Monstersound", Date: "2000", Kind: "single"}}}
	evidence := issuedTrackEvidence{
		"german": {{
			{Title: "Monstersound (radio mix)", DurationMS: 216760},
			{Title: "Monstersound (XTD club mix)", DurationMS: 462066},
			{Title: "Monstersound (Hot Floor mix)", DurationMS: 451173},
			{Title: "Monstersound (live mix)", DurationMS: 501760},
			{Title: "Monstersound (Pulsar Crew trance mix)", DurationMS: 597680},
			{Title: "Monstersound (Plug 'n' Play mix)", DurationMS: 457346},
			{Title: "Monstersound (Cosmic Gate mix)", DurationMS: 458986},
		}},
		"danish": {{
			{Title: "Monstersound (radio mix #1)", DurationMS: 218000},
			{Title: "Monstersound (XTD club mix)", DurationMS: 462000},
			{Title: "Monstersound (Hot Floor mix)", DurationMS: 451000},
			{Title: "Monstersound (live mix)", DurationMS: 503000},
			{Title: "Monstersound (Pulsar Crew trance mix)", DurationMS: 597000},
			{Title: "Monstersound (Plug'n'Play remix)", DurationMS: 456000},
			{Title: "Monstersound (Cosmic Gate remix)", DurationMS: 458000},
			{Title: "Monstersound (other remix)", DurationMS: 413000},
		}},
	}
	clusters := coalesceClustersWithIssuedTrackEvidence([]cluster{left, right}, evidence)
	if len(clusters) != 1 || len(clusters[0].Sources) != 2 {
		t.Fatalf("regional release groups were not coalesced: %#v", clusters)
	}
	if clusters[0].BridgeReason != "issued_release_track_overlap" || len(clusters[0].AlternateTargets) != 1 || clusters[0].AlternateTargets[0] != "danish" {
		t.Fatalf("track-overlap provenance was lost: %#v", clusters[0])
	}
}

func TestIssuedTrackOverlapDoesNotCollapseSingleTrackReleases(t *testing.T) {
	t.Parallel()
	left := cluster{TargetID: "left", Sources: []candidate{{Provider: "musicbrainz", Title: "Home", Date: "2024", Kind: "single"}}}
	right := cluster{TargetID: "right", Sources: []candidate{{Provider: "musicbrainz", Title: "Home", Date: "2024", Kind: "single"}}}
	evidence := issuedTrackEvidence{
		"left":  {{{Title: "Home", DurationMS: 180000}}},
		"right": {{{Title: "Home", DurationMS: 180000}}},
	}
	if got := len(coalesceClustersWithIssuedTrackEvidence([]cluster{left, right}, evidence)); got != 2 {
		t.Fatalf("one-track release groups collapsed without enough evidence: got %d", got)
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

func TestStorefrontIdentityCannotBeAnchoredByEarlierStorefront(t *testing.T) {
	t.Parallel()
	sets := map[string]map[string][]candidate{
		"musicbrainz": {"mb": {{Provider: "musicbrainz", Title: "Known", Kind: "album"}}},
		"apple": {"apple": {
			{Provider: "apple", Title: "Known", Kind: "album", ArtistName: "Artist"},
			{Provider: "apple", Title: "Storefront Only", Kind: "single", ArtistName: "Artist"},
		}},
		"deezer": {"deezer": {{Provider: "deezer", Title: "Storefront Only", Kind: "single", ArtistName: "Artist"}}},
	}
	selected := selectProviderIdentities(sets, []string{"Artist"})
	if len(selected["apple"]) != 2 {
		t.Fatalf("independently anchored Apple catalog was rejected: %#v", selected)
	}
	if len(selected["deezer"]) != 0 {
		t.Fatalf("Deezer catalog was bootstrapped from Apple: %#v", selected["deezer"])
	}
}

func TestOnlyCanonicalOrCorroboratedClustersArePublicDiscography(t *testing.T) {
	t.Parallel()
	if publicDiscographyCluster(cluster{Sources: []candidate{{Provider: "apple"}, {Provider: "deezer"}}}) {
		t.Fatal("storefront-only cluster became public")
	}
	if publicDiscographyCluster(cluster{Sources: []candidate{{Provider: "discogs"}}}) {
		t.Fatal("single-provider Discogs issue became public")
	}
	if !publicDiscographyCluster(cluster{PromotionState: "promoted", Sources: []candidate{{Provider: "discogs"}, {Provider: "apple"}}}) {
		t.Fatal("corroborated promoted cluster was not public")
	}
	if !publicDiscographyCluster(cluster{Sources: []candidate{{Provider: "musicbrainz"}}}) {
		t.Fatal("MusicBrainz release group was not public")
	}
}

func TestTwoIdentityGatedStorefrontsCanPromoteDigitalOnlyRelease(t *testing.T) {
	t.Parallel()
	group := cluster{Sources: []candidate{{Provider: "apple"}, {Provider: "deezer"}, {Provider: "lastfm"}}}
	if !promotableProviderCluster(group) {
		t.Fatal("independent storefront consensus could not promote a digital-only release")
	}
	if promotableProviderCluster(cluster{Sources: []candidate{{Provider: "discogs"}, {Provider: "lastfm"}}}) {
		t.Fatal("Last.fm incorrectly counted as independent catalog authority")
	}
}

func TestCatalogCreatedTargetCannotCanonizeItself(t *testing.T) {
	t.Parallel()
	if soleIndependentTarget(map[string]bool{"catalog-target": false}) {
		t.Fatal("catalog-created target became independent evidence")
	}
	if !soleIndependentTarget(map[string]bool{"provider-ingested-target": true}) {
		t.Fatal("independently ingested target was rejected")
	}
	if soleIndependentTarget(map[string]bool{"left": true, "right": true}) {
		t.Fatal("conflicting canonical targets were accepted")
	}
}

func TestFusedStorefrontCatalogKeepsOnlyAnchoredReleases(t *testing.T) {
	t.Parallel()
	anchors := make([]candidate, 19)
	for i := range anchors {
		anchors[i] = candidate{Provider: "musicbrainz", Title: fmt.Sprintf("Known %d", i), Kind: "single"}
	}
	values := make([]candidate, 0, 136)
	for i := 0; i < 7; i++ {
		values = append(values, candidate{Provider: "deezer", Title: fmt.Sprintf("Known %d", i), Kind: "single"})
	}
	for i := 7; i < 136; i++ {
		values = append(values, candidate{Provider: "deezer", Title: fmt.Sprintf("Namesake release %d", i), Kind: "single"})
	}
	kept, dropped := gateStorefrontCandidates(values, anchors)
	if len(kept) != 7 || dropped != 129 {
		t.Fatalf("kept=%d dropped=%d", len(kept), dropped)
	}
}

func TestPlausibleStorefrontCatalogKeepsDigitalOnlyReleases(t *testing.T) {
	t.Parallel()
	anchors := make([]candidate, 19)
	values := make([]candidate, 0, 40)
	for i := range anchors {
		anchors[i] = candidate{Provider: "musicbrainz", Title: fmt.Sprintf("Known %d", i), Kind: "single"}
		if i < 17 {
			values = append(values, candidate{Provider: "apple", Title: fmt.Sprintf("Known %d", i), Kind: "single"})
		}
	}
	for i := len(values); i < 40; i++ {
		values = append(values, candidate{Provider: "apple", Title: fmt.Sprintf("Digital single %d", i), Kind: "single"})
	}
	kept, dropped := gateStorefrontCandidates(values, anchors)
	if len(kept) != 40 || dropped != 0 {
		t.Fatalf("kept=%d dropped=%d", len(kept), dropped)
	}
}

func TestPlausibleStorefrontCatalogMarksDigitalOnlyReleaseAsIdentityGated(t *testing.T) {
	t.Parallel()
	selected := map[string][]candidate{
		"musicbrainz": {
			{Provider: "musicbrainz", Title: "Known Album", Kind: "album"},
			{Provider: "musicbrainz", Title: "Known Single", Kind: "single"},
		},
		"deezer": {
			{Provider: "deezer", Title: "Known Album", Kind: "album"},
			{Provider: "deezer", Title: "Oi AG!", Kind: "single"},
		},
	}
	selected, dropped := gateSelectedStorefronts(selected)
	if dropped != 0 || len(selected["deezer"]) != 2 {
		t.Fatalf("plausible catalog was gated: dropped=%d selected=%#v", dropped, selected["deezer"])
	}
	group := cluster{Sources: []candidate{selected["deezer"][1]}}
	if !promotableProviderCluster(group) {
		t.Fatal("fresh digital-only release from identity-gated Deezer page was not promotable")
	}
	if got := clusterConfidence(group); got < .8 {
		t.Fatalf("identity-gated storefront confidence: got %v", got)
	}
}

func TestFusedStorefrontCatalogCannotPromoteUnanchoredRelease(t *testing.T) {
	t.Parallel()
	anchors := make([]candidate, 19)
	values := make([]candidate, 0, 136)
	for i := range anchors {
		anchors[i] = candidate{Provider: "musicbrainz", Title: fmt.Sprintf("Known %d", i), Kind: "single"}
	}
	for i := 0; i < 7; i++ {
		values = append(values, candidate{Provider: "deezer", Title: fmt.Sprintf("Known %d", i), Kind: "single"})
	}
	values = append(values, candidate{Provider: "deezer", Title: "Wrong namesake release", Kind: "single"})
	for i := len(values); i < 136; i++ {
		values = append(values, candidate{Provider: "deezer", Title: fmt.Sprintf("Other namesake %d", i), Kind: "single"})
	}
	selected, _ := gateSelectedStorefronts(map[string][]candidate{"musicbrainz": anchors, "deezer": values})
	for _, value := range selected["deezer"] {
		if value.Title == "Wrong namesake release" {
			t.Fatal("unanchored release survived fused storefront gate")
		}
		if identityGatedStorefrontCluster(cluster{Sources: []candidate{value}}) {
			t.Fatal("weak fused catalog was marked identity-gated")
		}
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

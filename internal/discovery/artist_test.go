package discovery

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"testing"
)

func TestArtistHintsPromoteCorrectAmbiguousCandidate(t *testing.T) {
	request := NormalizeRequest(Request{Kind: KindArtist, Query: "ano", Hints: Hints{Country: "JP", Type: "Person", Releases: []ReleaseHint{{Title: "猫猫吐吐", Year: 2023}}}})
	japanese := Candidate{ProviderScore: 100, Display: Display{Name: "ano", Country: "JP", Type: "person"}, MatchedReleases: request.Hints.Releases}
	german := Candidate{ProviderScore: 100, Display: Display{Name: "ANO", Country: "DE", Type: "group"}}
	scoreCandidate(request, &japanese)
	scoreCandidate(request, &german)
	if japanese.Confidence <= german.Confidence || japanese.Match != "strong" {
		t.Fatalf("hints did not separate candidates: jp=%+v de=%+v", japanese, german)
	}
}
func TestRecommendationRequiresMargin(t *testing.T) {
	values := []Candidate{{Confidence: .91}, {Confidence: .86}}
	if got := recommendation(values); got != "ambiguous" {
		t.Fatalf("recommendation: %s", got)
	}
	values[1].Confidence = .70
	if got := recommendation(values); got != "strong_match" {
		t.Fatalf("recommendation: %s", got)
	}
}

func TestPresentCandidatesUsesFullSetForRecommendation(t *testing.T) {
	values := []Candidate{
		{Confidence: .91, Identity: ExternalID{Value: "first"}},
		{Confidence: .90, Identity: ExternalID{Value: "second"}},
	}
	recommended, presented := presentCandidates(values, 1)
	if recommended != "ambiguous" {
		t.Fatalf("recommendation with hidden runner-up = %q, want ambiguous", recommended)
	}
	if len(presented) != 1 || presented[0].Rank != 1 || presented[0].Identity.Value != "first" {
		t.Fatalf("presented candidates = %+v", presented)
	}
}

func TestWeakPartialArtistCandidatesAreNotExposedForReview(t *testing.T) {
	request := NormalizeRequest(Request{Kind: KindArtist, Query: "Above & Beyond ft Zoe Johnston"})
	partial := Candidate{ProviderScore: 100, Display: Display{Name: "Above"}}
	canonicalCredit := Candidate{ProviderScore: 95, Display: Display{Name: "Above & Beyond feat. Zoe Johnston"}}
	scoreCandidate(request, &partial)
	scoreCandidate(request, &canonicalCredit)
	filtered := filterWeakArtistCandidates(request, []Candidate{partial, canonicalCredit})
	if len(filtered) != 1 || filtered[0].Display.Name != canonicalCredit.Display.Name {
		t.Fatalf("weak partial artist was exposed: partial=%+v canonical=%+v filtered=%+v", partial, canonicalCredit, filtered)
	}
}

func TestWeakExactArtistCandidateRemainsReviewable(t *testing.T) {
	request := NormalizeRequest(Request{Kind: KindArtist, Query: "Ado"})
	candidate := Candidate{Display: Display{Name: "Ado"}}
	scoreCandidate(request, &candidate)
	if candidate.Match != "weak" {
		t.Fatalf("fixture stopped exercising weak exact candidate: %+v", candidate)
	}
	filtered := filterWeakArtistCandidates(request, []Candidate{candidate})
	if len(filtered) != 1 {
		t.Fatalf("exact low-provider-score candidate was lost: %+v", filtered)
	}
}

func TestPersistedReleaseClaimsAnchorMusicBrainzToExistingArtist(t *testing.T) {
	hint := ReleaseHint{Title: "Revelations", Year: 2005, Type: "album"}
	request := NormalizeRequest(Request{Kind: KindArtist, Query: "Audioslave", Hints: Hints{Releases: []ReleaseHint{hint}}})
	hintKey := releaseHintIdentityKey(request.Hints.Releases[0])
	mbid := "53dbf112-864c-4a77-8c37-e53bcb0c36fd"
	values := []Candidate{
		{Confidence: .85, Identity: ExternalID{Provider: "apple", Namespace: "artist", Value: "new-root"}, Display: Display{Name: "Audioslave"}, MatchedReleases: request.Hints.Releases, artistReleaseMatches: []artistReleaseMatch{{HintKey: hintKey, Artist: ExternalID{Provider: "apple", Namespace: "artist", Value: "new-root"}, Release: ExternalID{Provider: "apple", Namespace: "album", Value: "new-album"}}}},
		{Confidence: .85, Identity: ExternalID{Provider: "musicbrainz", Namespace: "artist", Value: mbid}, Display: Display{Name: "Audioslave"}, MatchedReleases: request.Hints.Releases, artistReleaseMatches: []artistReleaseMatch{{HintKey: hintKey, Artist: ExternalID{Provider: "musicbrainz", Namespace: "artist", Value: mbid}, Release: ExternalID{Provider: "musicbrainz", Namespace: "release_group", Value: "mb-release"}}}},
		{Confidence: .80, Identity: ExternalID{Provider: "deezer", Namespace: "artist", Value: "existing-root"}, ExistingEntityID: "canonical-artist", Display: Display{Name: "Audioslave"}, MatchedReleases: request.Hints.Releases, artistReleaseMatches: []artistReleaseMatch{{HintKey: hintKey, Artist: ExternalID{Provider: "deezer", Namespace: "artist", Value: "existing-root"}, Release: ExternalID{Provider: "deezer", Namespace: "album", Value: "existing-album"}}}},
	}
	checkerCalls := 0
	got, convergence, err := consolidateArtistCandidatesWithBridge(t.Context(), request, values, func(_ context.Context, bridge artistReleaseBridge) (bool, error) {
		checkerCalls++
		return bridge.EntityID == "canonical-artist" && bridge.MusicBrainz.Release.Value == "mb-release" && bridge.Storefront.Release.Value == "existing-album", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if checkerCalls != 1 || convergence == nil || convergence.EntityID != "canonical-artist" || convergence.MusicBrainzID != mbid {
		t.Fatalf("convergence=%+v calls=%d", convergence, checkerCalls)
	}
	if len(got) != 2 {
		t.Fatalf("new storefront root was incorrectly merged: %+v", got)
	}
	var anchored Candidate
	for _, candidate := range got {
		if candidate.ExistingEntityID == "canonical-artist" {
			anchored = candidate
		}
	}
	if anchored.Confidence != .99 || anchored.Match != "strong" {
		t.Fatalf("persisted bridge did not outrank search-only roots: %+v", anchored)
	}
	sort.Slice(got, func(i, j int) bool { return got[i].Confidence > got[j].Confidence })
	recommended, presented := presentCandidates(got, 10)
	result := Result{Kind: KindArtist, Recommendation: recommended, Candidates: presented, ArtistConvergence: convergence}
	FinalizeSearchResult(&result)
	if result.EntityID != "canonical-artist" || result.Status != "completed" || result.ArtistConvergence != convergence {
		t.Fatalf("hard convergence did not return the existing canonical: %+v", result)
	}
}

func TestTitleYearAndNewRootsNeverConsolidateArtists(t *testing.T) {
	hint := ReleaseHint{Title: "Greatest Hits", Year: 2020}
	request := NormalizeRequest(Request{Kind: KindArtist, Query: "Example", Hints: Hints{Releases: []ReleaseHint{hint}}})
	hintKey := releaseHintIdentityKey(request.Hints.Releases[0])
	values := []Candidate{
		{Identity: ExternalID{Provider: "musicbrainz", Namespace: "artist", Value: "one"}, Display: Display{Name: "Example"}, MatchedReleases: request.Hints.Releases, artistReleaseMatches: []artistReleaseMatch{{HintKey: hintKey, Release: ExternalID{Provider: "musicbrainz", Namespace: "release_group", Value: "release"}}}},
		{Identity: ExternalID{Provider: "apple", Namespace: "artist", Value: "two"}, Display: Display{Name: "Example"}, MatchedReleases: request.Hints.Releases, artistReleaseMatches: []artistReleaseMatch{{HintKey: hintKey, Release: ExternalID{Provider: "apple", Namespace: "album", Value: "album"}}}},
	}
	called := false
	got, convergence, err := consolidateArtistCandidatesWithBridge(t.Context(), request, values, func(context.Context, artistReleaseBridge) (bool, error) {
		called = true
		return true, nil
	})
	if err != nil || called || convergence != nil || len(got) != 2 {
		t.Fatalf("two new roots consolidated: got=%+v convergence=%+v called=%v err=%v", got, convergence, called, err)
	}
}

func TestStorefrontOnlyAmbiguityRemainsUnresolved(t *testing.T) {
	hint := ReleaseHint{Title: "Spinning Around", Year: 2023}
	request := NormalizeRequest(Request{Kind: KindArtist, Query: "MINNIE", Hints: Hints{Releases: []ReleaseHint{hint}}})
	hintKey := releaseHintIdentityKey(request.Hints.Releases[0])
	values := []Candidate{
		{Identity: ExternalID{Provider: "apple", Namespace: "artist", Value: "one"}, ExistingEntityID: "canonical", Display: Display{Name: "MINNIE"}, artistReleaseMatches: []artistReleaseMatch{{HintKey: hintKey, Release: ExternalID{Provider: "apple", Namespace: "album", Value: "one"}}}},
		{Identity: ExternalID{Provider: "deezer", Namespace: "artist", Value: "two"}, Display: Display{Name: "MINNIE"}, artistReleaseMatches: []artistReleaseMatch{{HintKey: hintKey, Release: ExternalID{Provider: "deezer", Namespace: "album", Value: "two"}}}},
	}
	got, convergence, err := consolidateArtistCandidatesWithBridge(t.Context(), request, values, func(context.Context, artistReleaseBridge) (bool, error) { return true, nil })
	if err != nil || convergence != nil || len(got) != len(values) {
		t.Fatalf("storefront-only ambiguity collapsed: got=%+v convergence=%+v err=%v", got, convergence, err)
	}
}
func TestNormalizeRequestMakesHintOrderDeterministic(t *testing.T) {
	left := NormalizeRequest(Request{Kind: " ARTIST ", Query: " Ado ", Hints: Hints{Aliases: []string{"アド", "Ado", "アド"}, Releases: []ReleaseHint{{Title: "残夢", Year: 2024}, {Title: "狂言", Year: 2022}}}})
	right := NormalizeRequest(Request{Kind: "artist", Query: "Ado", Hints: Hints{Aliases: []string{"Ado", "アド"}, Releases: []ReleaseHint{{Title: "狂言", Year: 2022}, {Title: "残夢", Year: 2024}}}})
	if left.Kind != right.Kind || left.Query != right.Query || len(left.Hints.Aliases) != 2 || left.Hints.Releases[0].Title != right.Hints.Releases[0].Title {
		t.Fatalf("requests differ: %+v / %+v", left, right)
	}
}
func TestNormalizedTextHandlesPunctuationAndNativeScript(t *testing.T) {
	if normalizedText("Blank & Jones") != "blankjones" || normalizedText("ハク。") != "ハク" {
		t.Fatal("normalization lost meaningful identity text")
	}
}
func TestRequestHashIgnoresHintOrdering(t *testing.T) {
	left := Request{Kind: "artist", Query: "Ado", Hints: Hints{Aliases: []string{"アド", "Ado"}, Releases: []ReleaseHint{{Title: "残夢", Year: 2024}, {Title: "狂言", Year: 2022}}}}
	right := Request{Kind: "artist", Query: "Ado", Hints: Hints{Aliases: []string{"Ado", "アド"}, Releases: []ReleaseHint{{Title: "狂言", Year: 2022}, {Title: "残夢", Year: 2024}}}}
	leftHash, leftJSON, err := RequestHash(left)
	if err != nil {
		t.Fatal(err)
	}
	rightHash, rightJSON, err := RequestHash(right)
	if err != nil {
		t.Fatal(err)
	}
	if leftHash != rightHash || !json.Valid(leftJSON) || !json.Valid(rightJSON) {
		t.Fatalf("hashes differ: %s %s", leftHash, rightHash)
	}
}
func TestReleaseHintEvidenceIsDeduplicated(t *testing.T) {
	hint := ReleaseHint{Title: "Monstersound", Year: 2001}
	values := appendUniqueReleaseHint(nil, hint)
	values = appendUniqueReleaseHint(values, hint)
	if len(values) != 1 {
		t.Fatalf("hints: %+v", values)
	}
}

func TestReleaseHintIdentifiersAreNormalizedMergedAndOrderIndependent(t *testing.T) {
	t.Parallel()
	left := NormalizeRequest(Request{Kind: KindArtist, Query: "Yoshiko", Hints: Hints{Releases: []ReleaseHint{
		{Title: "Freaks Out", Year: 2022, Type: "Single", Identifiers: []Identifier{{Scheme: "itunes_album", Value: "01630125755"}}},
		{Title: "Freaks Out", Year: 2022, Type: "single", Identifiers: []Identifier{{Scheme: "deezer_album", Value: "123"}}},
	}}})
	right := NormalizeRequest(Request{Kind: KindArtist, Query: "Yoshiko", Hints: Hints{Releases: []ReleaseHint{
		{Title: "Freaks Out", Year: 2022, Type: "single", Identifiers: []Identifier{{Scheme: "deezer", Value: "123"}, {Scheme: "apple", Value: "1630125755"}}},
	}}})
	if len(left.Hints.Releases) != 1 || len(left.Hints.Releases[0].Identifiers) != 2 {
		t.Fatalf("normalized releases: %#v", left.Hints.Releases)
	}
	leftHash, _, err := RequestHash(left)
	if err != nil {
		t.Fatal(err)
	}
	rightHash, _, err := RequestHash(right)
	if err != nil {
		t.Fatal(err)
	}
	if leftHash != rightHash {
		t.Fatalf("equivalent release identifiers produced different request hashes: %s != %s", leftHash, rightHash)
	}
}

func TestArtistReleaseSearchIsConstrainedToExactNameCandidates(t *testing.T) {
	var source mbArtistSearch
	if err := json.Unmarshal([]byte(`{"artists":[{"id":"056e4f3e-d505-4dad-8ec1-d04f521cbb56","name":"Daft Punk"}]}`), &source); err != nil {
		t.Fatal(err)
	}
	ids := releaseHintArtistIDs("Daft Punk", source)
	query := artistReleaseSearchQuery("Homework", ids)
	if !strings.Contains(query, `alias:"Homework"`) || !strings.Contains(query, `release:"Homework"`) || !strings.Contains(query, "arid:056e4f3e-d505-4dad-8ec1-d04f521cbb56") {
		t.Fatalf("query does not preserve title variants and artist identity: %s", query)
	}
}

func TestReleaseHintSearchHandlesColloquialTitles(t *testing.T) {
	titles := releaseHintSearchTitles("Cowboy Bebop OST 1")
	if len(titles) != 2 || titles[1] != "Cowboy Bebop" {
		t.Fatalf("search titles: %#v", titles)
	}
	if !releaseHintGroupMatches(ReleaseHint{Title: "Cowboy Bebop OST 1", Year: 1998, Type: "album"}, titles[1], true, "COWBOY BEBOP", "1998-05-21", "Album") {
		t.Fatal("soundtrack shorthand did not match the canonical release-group title")
	}
	if !releaseHintGroupMatches(ReleaseHint{Title: "Cross", Year: 2007, Type: "album"}, "Cross", false, "✝", "2007-06-06", "Album") {
		t.Fatal("alias-backed search evidence was discarded because the canonical title uses a symbol")
	}
	if releaseHintGroupMatches(ReleaseHint{Title: "Cross", Year: 2007, Type: "album"}, "Cross", false, "Cross", "2017", "Album") {
		t.Fatal("release year mismatch was accepted")
	}
	if releaseHintGroupMatches(ReleaseHint{Title: "Cross", Year: 2007, Type: "album"}, "Cross", false, "A Cross the Universe", "2008-11-24", "Album") {
		t.Fatal("a fuzzy Lucene result was treated as invisible alias evidence")
	}
	if !releaseHintGroupMatches(ReleaseHint{Title: "Torture Me (feat. Skrillex)", Year: 2022, Type: "album"}, "Torture Me (feat. Skrillex)", false, "Torture Me (feat. Skrillex)", "2022-12-02", "Single") {
		t.Fatal("an exact title/year was discarded because the local release type was wrong")
	}
}

func TestMovieHintsSeparateSameTitleRemakes(t *testing.T) {
	request := NormalizeRequest(Request{Kind: KindMovie, Query: "Dune", Hints: Hints{Year: 2021, Language: "en"}})
	current := Candidate{ProviderScore: 100, Display: Display{Title: "Dune", Year: 2021, Language: "en"}}
	older := Candidate{ProviderScore: 96, Display: Display{Title: "Dune", Year: 1984, Language: "en"}}
	scoreMovieCandidate(request, &current)
	scoreMovieCandidate(request, &older)
	if current.Confidence <= older.Confidence || current.Match != "likely" {
		t.Fatalf("movie hints did not separate remakes: current=%+v older=%+v", current, older)
	}
}

func TestReleaseGroupHintsSeparateSameTitleReleases(t *testing.T) {
	request := NormalizeRequest(Request{Kind: KindReleaseGroup, Query: "Greatest Hits", Hints: Hints{Year: 1980, Type: "album", Artists: []string{"Queen"}, Tracks: []string{"Another One Bites the Dust"}}})
	queen := Candidate{ProviderScore: 100, Display: Display{Title: "Greatest Hits", Year: 1980, Type: "album", Artists: []ArtistDisplay{{ID: "queen", Name: "Queen"}}}, MatchedTracks: request.Hints.Tracks}
	other := Candidate{ProviderScore: 100, Display: Display{Title: "Greatest Hits", Year: 1990, Type: "album", Artists: []ArtistDisplay{{ID: "other", Name: "Other"}}}}
	scoreReleaseGroupCandidate(request, &queen)
	scoreReleaseGroupCandidate(request, &other)
	if queen.Confidence <= other.Confidence || queen.Match != "strong" {
		t.Fatalf("release-group hints did not separate releases: queen=%+v other=%+v", queen, other)
	}
}

func TestNormalizeRequestMakesCrossDomainHintsDeterministic(t *testing.T) {
	left := NormalizeRequest(Request{Kind: KindReleaseGroup, Query: "Album", Hints: Hints{Language: " EN ", Artists: []string{"Queen", "queen"}, ArtistIDs: []string{"B10BBBFC-CF9E-42E0-BE17-E2C3E1D2600D"}, Tracks: []string{"Two", "One"}}})
	right := NormalizeRequest(Request{Kind: KindReleaseGroup, Query: "Album", Hints: Hints{Language: "en", Artists: []string{"Queen"}, ArtistIDs: []string{"b10bbbfc-cf9e-42e0-be17-e2c3e1d2600d"}, Tracks: []string{"One", "Two"}}})
	leftHash, _, err := RequestHash(left)
	if err != nil {
		t.Fatal(err)
	}
	rightHash, _, err := RequestHash(right)
	if err != nil {
		t.Fatal(err)
	}
	if leftHash != rightHash {
		t.Fatalf("normalized cross-domain hints differ: %+v / %+v", left, right)
	}
}

func TestAniDBTitleDumpParsing(t *testing.T) {
	values, err := parseAnimeTitleDump([]byte(`<animetitles><anime aid="23"><title type="main" xml:lang="x-jat">Cowboy Bebop</title><title type="official" xml:lang="ja">カウボーイビバップ</title></anime></animetitles>`))
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 1 || values[0].AID != "23" || len(values[0].Titles) != 2 {
		t.Fatalf("titles: %+v", values)
	}
}

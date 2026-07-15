package discovery

import (
	"context"
	"testing"

	"github.com/HeyaMedia/HeyaMetadata/internal/providercredentials"
)

func TestIdentifierNormalizationIsOrderIndependent(t *testing.T) {
	left := Request{
		Kind: " TV_SHOW ",
		Identifiers: []Identifier{
			{Scheme: "TMDB_ID", Value: "001396"},
			{Scheme: "IMDb_ID", Value: " TT0903747 "},
			{Scheme: "tmdb", Value: "1396"},
		},
	}
	right := Request{
		Kind: KindTVShow,
		Identifiers: []Identifier{
			{Scheme: "imdb", Value: "tt0903747"},
			{Scheme: "tmdb", Value: "1396"},
		},
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
		t.Fatalf("identifier ordering or aliases changed request identity: %s != %s", leftHash, rightHash)
	}
	normalized := NormalizeRequest(left)
	if len(normalized.Identifiers) != 2 {
		t.Fatalf("identifiers: got %#v", normalized.Identifiers)
	}
	if normalized.Identifiers[0] != (Identifier{Scheme: "imdb", Value: "tt0903747"}) || normalized.Identifiers[1] != (Identifier{Scheme: "tmdb", Value: "1396"}) {
		t.Fatalf("normalized identifiers: %#v", normalized.Identifiers)
	}
}

func TestIdentifierClaimTargetsStayBehindCanonicalKinds(t *testing.T) {
	tests := []struct {
		kind       string
		identifier Identifier
		want       claimTarget
	}{
		{KindMovie, Identifier{Scheme: "imdb", Value: "tt0133093"}, claimTarget{EntityKind: KindMovie, Provider: "imdb", Namespace: "title"}},
		{KindTVShow, Identifier{Scheme: "tvdb", Value: "81189"}, claimTarget{EntityKind: KindTVShow, Provider: "tvdb", Namespace: "series"}},
		{KindAnime, Identifier{Scheme: "myanimelist", Value: "1"}, claimTarget{EntityKind: KindAnime, Provider: "myanimelist", Namespace: "anime"}},
		{KindArtist, Identifier{Scheme: "musicbrainz", Value: "e134b52f-2e9e-4734-9bc3-bea9648d1fa1"}, claimTarget{EntityKind: KindArtist, Provider: "musicbrainz", Namespace: "artist"}},
		{KindBookWork, Identifier{Scheme: "isbn", Value: "9780261102217"}, claimTarget{EntityKind: KindBookWork, Provider: "isbn", Namespace: "isbn13", ViaWork: true}},
	}
	for _, test := range tests {
		got, ok := claimTargetFor(test.kind, test.identifier)
		if !ok {
			t.Fatalf("%s/%s was not supported", test.kind, test.identifier.Scheme)
		}
		if got != test.want {
			t.Fatalf("%s/%s: got %#v, want %#v", test.kind, test.identifier.Scheme, got, test.want)
		}
	}
	if _, ok := claimTargetFor(KindMovie, Identifier{Scheme: "musicbrainz", Value: "irrelevant"}); ok {
		t.Fatal("a music identifier must not be interpreted for movies")
	}
}

func TestISBNNormalization(t *testing.T) {
	got := normalizeIdentifier(Identifier{Scheme: "ISBN", Value: "978-0-261-10221-7"})
	if got != (Identifier{Scheme: "isbn", Value: "9780261102217"}) {
		t.Fatalf("got %#v", got)
	}
}

func TestArtistReleaseIdentifiersRequireDurableIdentityCheck(t *testing.T) {
	request := NormalizeRequest(Request{Kind: KindArtist, Hints: Hints{Releases: []ReleaseHint{{Title: "Vault Playlist, Vol. 1", Identifiers: []Identifier{{Scheme: "musicbrainz", Value: "ffd21680-ae04-4e8b-8523-0a5c1001627b"}}}}}})
	if !hasArtistReleaseIdentityEvidence(request) {
		t.Fatal("MusicBrainz release identity evidence must bypass the synchronous known-ID shortcut")
	}
	request.Kind = KindMovie
	if hasArtistReleaseIdentityEvidence(request) {
		t.Fatal("artist release routing must not affect movie discovery")
	}
}

func TestInvalidMusicBrainzIdentifiersRemainUnusedEvidence(t *testing.T) {
	t.Parallel()
	service := &Service{}
	request := Request{Kind: KindArtist, Identifiers: []Identifier{
		{Scheme: "musicbrainz", Value: "digital media"},
		{Scheme: "musicbrainz", Value: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaa8910786"},
	}}
	result, handled, err := service.ResolveFreshIdentifiers(context.Background(), request, 0, providercredentials.Credentials{})
	if err != nil {
		t.Fatal(err)
	}
	if !handled || result.EntityID != "" || len(result.IdentifierEvidence) != 2 {
		t.Fatalf("result=%+v handled=%v", result, handled)
	}
	for _, evidence := range result.IdentifierEvidence {
		if evidence.Outcome != "unused" || evidence.Detail != "identifier value is invalid for scheme" {
			t.Fatalf("invalid identifier escaped validation: %+v", evidence)
		}
	}
	if root, ok := directIngestionRoot(KindReleaseGroup, Identifier{Scheme: "musicbrainz", Value: "bbbbbbbb-bbbb-bbbb-bbbb-bbb301168317"}); ok {
		t.Fatalf("synthetic release-group ID became an ingestion root: %+v", root)
	}
}

func TestInvalidArtistReleaseIdentifiersDoNotTriggerProviderRouting(t *testing.T) {
	t.Parallel()
	request := NormalizeRequest(Request{Kind: KindArtist, Hints: Hints{Releases: []ReleaseHint{{
		Title: "Synthetic release",
		Identifiers: []Identifier{
			{Scheme: "musicbrainz", Value: "bbbbbbbb-bbbb-bbbb-bbbb-bbb301168317"},
			{Scheme: "apple", Value: "not-an-id"},
		},
	}}}})
	if hasArtistReleaseIdentityEvidence(request) {
		t.Fatal("invalid release identifiers forced durable provider routing")
	}
	roots, err := (&Service{}).artistRootsFromReleaseHints(context.Background(), request.Hints.Releases, 0, providercredentials.Credentials{})
	if err != nil || len(roots) != 0 {
		t.Fatalf("roots=%+v err=%v", roots, err)
	}
}

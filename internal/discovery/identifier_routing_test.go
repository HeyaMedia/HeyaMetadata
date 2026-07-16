package discovery

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/HeyaMedia/HeyaMetadata/internal/config"
	rgdomain "github.com/HeyaMedia/HeyaMetadata/internal/domains/releasegroup"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/musicbrainz"
)

func TestArtistReleaseEvidenceRoutesThroughCreditedMusicBrainzArtist(t *testing.T) {
	releaseGroupID := "ffd21680-ae04-4e8b-8523-0a5c1001627b"
	artistID := "8ef1df30-ae4f-4dbd-9351-1a32b208a01e"
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		if request.URL.Path != "/release-group/"+releaseGroupID {
			response.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(response).Encode(map[string]any{"error": "not found"})
			return
		}
		_ = json.NewEncoder(response).Encode(map[string]any{
			"id": releaseGroupID, "title": "Vault Playlist, Vol. 1", "first-release-date": "2017-04-07", "primary-type": "EP",
			"artist-credit": []any{map[string]any{"artist": map[string]any{"id": artistID, "name": "Alicia Keys"}}},
		})
	}))
	t.Cleanup(server.Close)
	client := musicbrainz.New(config.MusicBrainzConfig{BaseURL: server.URL, RequestsPerSecond: 1000, UserAgent: "HeyaMetadata/test"})

	evidence, matched, err := artistReleaseEvidenceFromMusicBrainz(t.Context(), client, ReleaseHint{Title: "Vault Playlist, Vol. 1", Year: 2017, Type: "ep"}, Identifier{Scheme: "musicbrainz", Value: releaseGroupID})
	if err != nil {
		t.Fatal(err)
	}
	if !matched || len(evidence.Credits) != 1 || evidence.Credits[0].Root != (ingestionRoot{Kind: KindArtist, Provider: "musicbrainz", Namespace: "artist", Value: artistID}) {
		t.Fatalf("evidence=%+v matched=%v", evidence, matched)
	}

	mismatched, matched, err := artistReleaseEvidenceFromMusicBrainz(t.Context(), client, ReleaseHint{Title: "The Hits Collection", Year: 2010, Type: "album"}, Identifier{Scheme: "musicbrainz", Value: releaseGroupID})
	if err != nil || matched || len(mismatched.Credits) != 0 {
		t.Fatalf("mismatched evidence=%+v matched=%v err=%v", mismatched, matched, err)
	}
}

func TestArtistIngestionRootsPreferMusicBrainzBeforeStorefronts(t *testing.T) {
	values := map[string]ingestionRoot{}
	for _, root := range []ingestionRoot{
		{Kind: KindArtist, Provider: "deezer", Namespace: "artist", Value: "228"},
		{Kind: KindArtist, Provider: "musicbrainz", Namespace: "artist", Value: "8ef1df30-ae4f-4dbd-9351-1a32b208a01e"},
		{Kind: KindArtist, Provider: "apple", Namespace: "artist", Value: "316069"},
	} {
		values[root.key()] = root
	}
	ordered := sortedIngestionRoots(values)
	if len(ordered) != 3 || ordered[0].Provider != "musicbrainz" || ordered[1].Provider != "apple" || ordered[2].Provider != "deezer" {
		t.Fatalf("ordered roots=%+v", ordered)
	}
}

func TestStorefrontReleaseCreditsBecomeArtistRootsOnlyWhenReleaseMatches(t *testing.T) {
	record := rgdomain.NormalizedRecordV1{
		ProviderRecord: rgdomain.ProviderRecord{Provider: "apple", Namespace: "album", Value: "1440984578"},
		Titles:         []rgdomain.Title{{Value: "The Hits Collection, Vol. One", Primary: true}},
		Dates:          []rgdomain.DateValue{{Value: "2010-11-22"}},
		Classification: rgdomain.Classification{PrimaryType: "album"},
		ArtistCredits:  []rgdomain.ArtistCredit{{ArtistProvider: "apple", ArtistNamespace: "artist", ArtistID: "1352449404", ArtistName: "JAŸ-Z"}},
	}
	matched, ok, err := artistReleaseEvidenceFromNormalizedRecord(ReleaseHint{Title: "The Hits Collection, Vol. One", Year: 2010, Type: "ep"}, Identifier{Scheme: "apple", Value: "1440984578"}, record)
	if err != nil || !ok || len(matched.Credits) != 1 || matched.Credits[0].Root != (ingestionRoot{Kind: KindArtist, Provider: "apple", Namespace: "artist", Value: "1352449404"}) {
		t.Fatalf("matched evidence=%+v ok=%v err=%v", matched, ok, err)
	}
	if evidence, ok, err := artistReleaseEvidenceFromNormalizedRecord(ReleaseHint{Title: "Vault Playlist, Vol. 1", Year: 2017, Type: "ep"}, Identifier{Scheme: "apple", Value: "1440984578"}, record); err != nil || ok || len(evidence.Credits) != 0 {
		t.Fatalf("mismatched storefront release produced evidence=%+v ok=%v err=%v", evidence, ok, err)
	}
}

func TestCollaborativeReleaseCreditsAreFilteredByRequestedArtist(t *testing.T) {
	hyolynID := "75aff57c-d397-4e6c-b5fc-1f32d7761512"
	changmoID := "93ed67ff-2ab4-4fb3-9e3c-b971b8481e1a"
	evidence := []artistReleaseEvidence{{
		Hint:       ReleaseHint{Title: "BLUE MOON", Year: 2017, Type: "single"},
		Identifier: Identifier{Scheme: "musicbrainz", Value: "1cfefaf8-b278-4f0e-a112-a1d9339c4ea4"},
		Credits: []artistReleaseCredit{
			{Root: ingestionRoot{Kind: KindArtist, Provider: "musicbrainz", Namespace: "artist", Value: hyolynID}, Names: []string{"HYOLYN", "Hyolyn", "효린"}},
			{Root: ingestionRoot{Kind: KindArtist, Provider: "musicbrainz", Namespace: "artist", Value: changmoID}, Names: []string{"CHANGMO", "창모"}},
		},
	}}

	selected := selectUnanchoredArtistReleaseRoots(Request{Kind: KindArtist, Query: "효린", Hints: Hints{Aliases: []string{"Hyolyn"}}}, evidence)
	if len(selected) != 1 || selected[(ingestionRoot{Kind: KindArtist, Provider: "musicbrainz", Namespace: "artist", Value: hyolynID}).key()].Value != hyolynID {
		t.Fatalf("Hyolyn selection=%+v", selected)
	}
	selected = selectUnanchoredArtistReleaseRoots(Request{Kind: KindArtist, Query: "CHANGMO"}, evidence)
	if len(selected) != 1 || selected[(ingestionRoot{Kind: KindArtist, Provider: "musicbrainz", Namespace: "artist", Value: changmoID}).key()].Value != changmoID {
		t.Fatalf("Changmo selection=%+v", selected)
	}
}

func TestReleaseContributorRolesCannotBecomeArtistRoots(t *testing.T) {
	evidence := []artistReleaseEvidence{{Credits: []artistReleaseCredit{
		{Root: ingestionRoot{Kind: KindArtist, Provider: "deezer", Namespace: "artist", Value: "1"}, Names: []string{"Main Artist"}, Role: "main"},
		{Root: ingestionRoot{Kind: KindArtist, Provider: "deezer", Namespace: "artist", Value: "2"}, Names: []string{"Guest Artist"}, Role: "featured"},
		{Root: ingestionRoot{Kind: KindArtist, Provider: "discogs", Namespace: "artist", Value: "3"}, Names: []string{"Studio Producer"}, Role: "producer"},
		{Root: ingestionRoot{Kind: KindArtist, Provider: "discogs", Namespace: "artist", Value: "4"}, Names: []string{"Mix Engineer"}, Role: "mixed_by"},
	}}}

	for _, test := range []struct {
		query string
		want  string
	}{
		{query: "Main Artist", want: "1"},
		{query: "Guest Artist", want: "2"},
		{query: "Studio Producer"},
		{query: "Mix Engineer"},
	} {
		selected := selectUnanchoredArtistReleaseRoots(Request{Kind: KindArtist, Query: test.query}, evidence)
		if test.want == "" && len(selected) != 0 {
			t.Fatalf("%s became identity roots: %+v", test.query, selected)
		}
		if test.want != "" {
			if len(selected) != 1 {
				t.Fatalf("%s selection=%+v", test.query, selected)
			}
			for _, root := range selected {
				if root.Value != test.want {
					t.Fatalf("%s root=%+v, want %s", test.query, root, test.want)
				}
			}
		}
	}
}

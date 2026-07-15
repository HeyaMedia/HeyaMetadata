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

	roots, err := artistRootsFromMusicBrainzRelease(t.Context(), client, ReleaseHint{Title: "Vault Playlist, Vol. 1", Year: 2017, Type: "ep"}, releaseGroupID)
	if err != nil {
		t.Fatal(err)
	}
	if len(roots) != 1 || roots[0] != (ingestionRoot{Kind: KindArtist, Provider: "musicbrainz", Namespace: "artist", Value: artistID}) {
		t.Fatalf("roots=%+v", roots)
	}

	mismatched, err := artistRootsFromMusicBrainzRelease(t.Context(), client, ReleaseHint{Title: "The Hits Collection", Year: 2010, Type: "album"}, releaseGroupID)
	if err != nil || len(mismatched) != 0 {
		t.Fatalf("mismatched roots=%+v err=%v", mismatched, err)
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
	matched := artistRootsFromNormalizedRelease(ReleaseHint{Title: "The Hits Collection, Vol. One", Year: 2010, Type: "ep"}, record)
	if len(matched) != 1 || matched[0] != (ingestionRoot{Kind: KindArtist, Provider: "apple", Namespace: "artist", Value: "1352449404"}) {
		t.Fatalf("matched roots=%+v", matched)
	}
	if roots := artistRootsFromNormalizedRelease(ReleaseHint{Title: "Vault Playlist, Vol. 1", Year: 2017, Type: "ep"}, record); len(roots) != 0 {
		t.Fatalf("mismatched storefront release produced roots=%+v", roots)
	}
}

package discovery

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/providers"
)

type artistRelationshipCollectorStub struct {
	payloads map[string]providers.Payload
	calls    []string
}

func (collector *artistRelationshipCollectorStub) Collect(_ context.Context, identifier providers.Identifier) ([]providers.Payload, error) {
	collector.calls = append(collector.calls, identifier.Value)
	payload, ok := collector.payloads[identifier.Value]
	if !ok {
		return nil, fmt.Errorf("unexpected artist %s", identifier.Value)
	}
	return []providers.Payload{payload}, nil
}

func TestCollectExplicitMusicBrainzArtistLinksExpandsOnlyExactHits(t *testing.T) {
	const mbid = "a6fd2c10-cc3e-41f2-8323-28851e6a48f0"
	collector := &artistRelationshipCollectorStub{payloads: map[string]providers.Payload{
		mbid: {
			StatusCode: http.StatusOK,
			ObservedAt: time.Unix(1, 0).UTC(),
			Body: []byte(`{
				"id":"a6fd2c10-cc3e-41f2-8323-28851e6a48f0",
				"name":"Unlucky Morpheus",
				"relations":[
					{"target-type":"url","type":"streaming","url":{"resource":"https://music.apple.com/jp/artist/1376471888"}},
					{"target-type":"url","type":"free streaming","url":{"resource":"https://www.deezer.com/artist/14633177"}},
					{"target-type":"url","type":"free streaming","url":{"resource":"https://open.spotify.com/artist/3NyKjyguig68xw6kSpeDiW"}},
					{"target-type":"url","type":"streaming","url":{"resource":"https://tidal.com/artist/32745875"}}
				]
			}`),
		},
	}}
	request := NormalizeRequest(Request{Kind: KindArtist, Query: "Unlucky Morpheus"})
	values := []Candidate{
		{Identity: ExternalID{Provider: "musicbrainz", Namespace: "artist", Value: mbid}, Display: Display{Name: "Unlucky Morpheus"}},
		{Identity: ExternalID{Provider: "musicbrainz", Namespace: "artist", Value: "f59c5520-5f46-4d2c-b2c4-822eabf53419"}, Display: Display{Name: "Something Else"}},
	}

	links, failures := collectExplicitMusicBrainzArtistLinks(t.Context(), collector, request, values)
	if len(failures) != 0 || len(collector.calls) != 1 || collector.calls[0] != mbid {
		t.Fatalf("calls=%v failures=%v", collector.calls, failures)
	}
	got := map[string]string{}
	for _, link := range links[mbid] {
		got[link.Provider] = link.Value
	}
	want := map[string]string{"apple": "1376471888", "deezer": "14633177", "spotify": "3NyKjyguig68xw6kSpeDiW", "tidal": "32745875"}
	for provider, value := range want {
		if got[provider] != value {
			t.Fatalf("%s link=%q, all=%v", provider, got[provider], got)
		}
	}
}

func TestExplicitMusicBrainzLinksCollapseStorefrontCandidates(t *testing.T) {
	const mbid = "a6fd2c10-cc3e-41f2-8323-28851e6a48f0"
	values := []Candidate{
		{
			Identity:      ExternalID{Provider: "musicbrainz", Namespace: "artist", Value: mbid},
			Resolution:    Resolution{Kind: KindArtist, Provider: "musicbrainz", Namespace: "artist", Value: mbid},
			ProviderScore: 100,
			Confidence:    .6,
			Match:         "possible",
			Display:       Display{Name: "Unlucky Morpheus", Type: "group", Area: "Japan", BeginDate: "2008", Genres: []string{"j-rock", "metal"}},
			Evidence:      []Evidence{{Field: "name", Outcome: "exact", Weight: .38}},
		},
		{
			Identity:      ExternalID{Provider: "apple", Namespace: "artist", Value: "1376471888"},
			Resolution:    Resolution{Kind: KindArtist, Provider: "apple", Namespace: "artist", Value: "1376471888"},
			ProviderScore: 100,
			Display:       Display{Name: "Unlucky Morpheus", Type: "artist", Genres: []string{"Rock"}},
		},
		{
			Identity:         ExternalID{Provider: "deezer", Namespace: "artist", Value: "14633177"},
			Resolution:       Resolution{Kind: KindArtist, Provider: "deezer", Namespace: "artist", Value: "14633177"},
			ProviderScore:    100,
			ExistingEntityID: "storefront-artist",
			Display:          Display{Name: "Unlucky Morpheus", Type: "artist", ImageURL: "https://images.example/artist.jpg", ImageWidth: 1000, ImageHeight: 1000, ReleaseCount: 28, FanCount: 4707},
		},
	}
	links := map[string][]ExternalID{
		mbid: {
			{Provider: "apple", Namespace: "artist", Value: "1376471888"},
			{Provider: "deezer", Namespace: "artist", Value: "14633177"},
			{Provider: "spotify", Namespace: "artist", Value: "3NyKjyguig68xw6kSpeDiW"},
			{Provider: "tidal", Namespace: "artist", Value: "32745875"},
		},
	}

	got := consolidateExplicitMusicBrainzArtistLinks(values, links)
	if len(got) != 1 {
		t.Fatalf("candidates=%+v", got)
	}
	candidate := got[0]
	if candidate.Identity.Provider != "musicbrainz" || candidate.Resolution.Provider != "musicbrainz" || candidate.Resolution.Value != mbid {
		t.Fatalf("resolution did not stay on MusicBrainz spine: %+v", candidate)
	}
	if candidate.ExistingEntityID != "" {
		t.Fatalf("new MusicBrainz relationship skipped ingestion through existing entity %q", candidate.ExistingEntityID)
	}
	if candidate.Confidence != .99 || candidate.Match != "strong" {
		t.Fatalf("explicit crosswalk was not decisive: %+v", candidate)
	}
	if candidate.Display.Type != "group" || candidate.Display.Area != "Japan" || candidate.Display.BeginDate != "2008" {
		t.Fatalf("MusicBrainz facts were lost: %+v", candidate.Display)
	}
	if candidate.Display.ImageURL == "" || candidate.Display.ReleaseCount != 28 || candidate.Display.FanCount != 4707 {
		t.Fatalf("storefront presentation was not merged: %+v", candidate.Display)
	}
	if len(candidate.Display.Genres) != 3 || candidate.Display.Genres[2] != "Rock" {
		t.Fatalf("genres=%v", candidate.Display.Genres)
	}
	if len(candidate.Evidence) != 2 || candidate.Evidence[1].Field != "identity_crosswalk" {
		t.Fatalf("evidence=%+v", candidate.Evidence)
	}
}

func TestSharedStorefrontLinkRemainsAmbiguous(t *testing.T) {
	values := []Candidate{
		{Identity: ExternalID{Provider: "musicbrainz", Namespace: "artist", Value: "one"}, Display: Display{Name: "Shared"}},
		{Identity: ExternalID{Provider: "musicbrainz", Namespace: "artist", Value: "two"}, Display: Display{Name: "Shared"}},
		{Identity: ExternalID{Provider: "deezer", Namespace: "artist", Value: "three"}, Display: Display{Name: "Shared"}},
	}
	links := map[string][]ExternalID{
		"one": {{Provider: "deezer", Namespace: "artist", Value: "three"}},
		"two": {{Provider: "deezer", Namespace: "artist", Value: "three"}},
	}
	if got := consolidateExplicitMusicBrainzArtistLinks(values, links); len(got) != len(values) {
		t.Fatalf("shared upstream root was collapsed: %+v", got)
	}
}

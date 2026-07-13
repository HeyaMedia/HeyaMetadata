package artist

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestCombineUsesMusicBrainzSpineAndRetainsSupplementalEvidence(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	mb := NormalizedRecordV1{ProviderRecord: ProviderRecord{Provider: "musicbrainz", PrimaryObservationID: "mb", ObservedAt: now}, IdentityCandidates: []IdentityCandidate{{Provider: "musicbrainz", Namespace: "artist", NormalizedValue: "mbid"}, {Provider: "discogs", Namespace: "artist", NormalizedValue: "82730"}}, Names: []Name{{Value: "The Beatles", Type: "display", Primary: true}}, Classification: Classification{ArtistType: "group"}, Genres: []WeightedTerm{{Name: "rock", Weight: 47}}}
	discogs := NormalizedRecordV1{ProviderRecord: ProviderRecord{Provider: "discogs", PrimaryObservationID: "discogs", ObservedAt: now}, IdentityCandidates: []IdentityCandidate{{Provider: "discogs", Namespace: "artist", NormalizedValue: "82730"}}, Biographies: []Text{{Value: "English rock band", Type: "provider_profile"}}, Images: []Image{{ProviderImageID: "primary", SourceURL: "https://img.discogs.com/private.jpg", Class: "primary"}}, Relationships: []Relationship{{Type: "member", TargetProvider: "discogs", TargetNamespace: "artist", TargetID: "46481", TargetName: "John Lennon"}}}
	lastfm := NormalizedRecordV1{ProviderRecord: ProviderRecord{Provider: "lastfm", PrimaryObservationID: "lastfm", ObservedAt: now}, Images: []Image{{ProviderImageID: "star", Class: "extralarge"}}, Metrics: []Metric{{Name: "listeners", Value: 6000000}}, SimilarArtists: []SimilarArtist{{Name: "John Lennon"}}}
	projection := Combine("entity", "the-beatles", 1, []RecordInput{{ID: "lastfm-record", Record: lastfm}, {ID: "discogs-record", Record: discogs}, {ID: "mb-record", Record: mb}}, map[string]string{"discogs:primary:primary": "opaque-image"}, now)
	if projection.Detail.Display.Name != "The Beatles" || projection.Detail.Display.ImageID != "opaque-image" || len(projection.Detail.ExternalIDs) != 2 || len(projection.Detail.Data.Images) != 1 || len(projection.Detail.Data.Metrics) != 1 || len(projection.Detail.Data.Relationships) != 1 {
		t.Fatalf("projection: %+v", projection.Detail)
	}
	body, _ := json.Marshal(projection.Detail)
	if strings.Contains(string(body), "img.discogs.com") {
		t.Fatal("public projection leaked upstream image URL")
	}
}

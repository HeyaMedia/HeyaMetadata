package releasegroup

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestEquivalentEditionsCollapseAcrossScriptsAndRetainSources(t *testing.T) {
	values := []ProjectedEdition{{Provider: "apple", Namespace: "album", ProviderID: "1", Title: "初夏", Date: DateValue{Value: "2024-06-01"}, TrackCount: 10, Sources: []EditionSource{{Provider: "apple", Namespace: "album", ProviderID: "1"}}}}
	incoming := ProjectedEdition{Provider: "deezer", Namespace: "album", ProviderID: "2", Title: "Shoka", Date: DateValue{Value: "2024-06-02"}, TrackCount: 10, Sources: []EditionSource{{Provider: "deezer", Namespace: "album", ProviderID: "2"}}}
	index := equivalentEditionIndex(values, incoming)
	if index != 0 {
		t.Fatalf("equivalent edition index: %d", index)
	}
	values[index].Sources = append(values[index].Sources, incoming.Sources...)
	if len(values) != 1 || len(values[0].Sources) != 2 {
		t.Fatalf("edition sources: %+v", values)
	}
	remix := incoming
	remix.Title = "Shoka (Remix)"
	if equivalentEditionIndex(values, remix) >= 0 {
		t.Fatal("remix collapsed into base release")
	}
}

func TestCombineKeepsWorkAndProviderEditionsSeparate(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	mb := NormalizedRecordV1{ProviderRecord: ProviderRecord{Provider: "musicbrainz", PrimaryObservationID: "mb", ObservedAt: now}, IdentityCandidates: []IdentityCandidate{{Provider: "musicbrainz", Namespace: "release_group", NormalizedValue: "rg"}}, Titles: []Title{{Value: "Abbey Road", Type: "display", Primary: true}}, ArtistCredits: []ArtistCredit{{Name: "The Beatles", ArtistID: "artist"}}, Classification: Classification{PrimaryType: "album"}, Dates: []DateValue{{Value: "1969-09-26", Precision: "day", Type: "first_release"}}, Editions: []Edition{{Provider: "musicbrainz", Namespace: "release", ProviderID: "release", Title: "Abbey Road"}}}
	apple := NormalizedRecordV1{ProviderRecord: ProviderRecord{Provider: "apple", PrimaryObservationID: "apple", ObservedAt: now}, IdentityCandidates: []IdentityCandidate{{Provider: "apple", Namespace: "album", NormalizedValue: "1441164426"}}, Titles: []Title{{Value: "Abbey Road (Remastered)", Type: "edition_title", Primary: true}}, Images: []Image{{ProviderImageID: "cover", SourceURL: "https://is1/cover.jpg", Class: "cover"}}, Editions: []Edition{{Provider: "apple", Namespace: "album", ProviderID: "1441164426", Title: "Abbey Road (Remastered)", Image: &Image{ProviderImageID: "cover", SourceURL: "https://is1/cover.jpg", Class: "cover"}}}}
	projection := Combine("entity", "abbey-road-1969", 1, []RecordInput{{ID: "apple-record", Record: apple}, {ID: "mb-record", Record: mb}}, map[string]string{"apple:cover:cover": "opaque"}, now)
	if projection.Detail.Display.Title != "Abbey Road" || projection.Detail.Display.ArtistCredit != "The Beatles" || projection.Detail.Display.Year != 1969 || projection.Detail.Display.ImageID != "opaque" || len(projection.Detail.Data.Editions) != 2 {
		t.Fatalf("projection: %+v", projection.Detail)
	}
	body, _ := json.Marshal(projection.Detail)
	if strings.Contains(string(body), "is1/cover.jpg") {
		t.Fatal("projection leaked upstream image URL")
	}
}

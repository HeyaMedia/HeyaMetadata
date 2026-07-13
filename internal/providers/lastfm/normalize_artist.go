package lastfm

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	artistdomain "github.com/HeyaMedia/HeyaMetadata/internal/domains/artist"
)

func NormalizeArtist(body []byte, expectedMBID string, expectedNames []string, observationID string, observedAt time.Time) (artistdomain.NormalizedRecordV1, error) {
	var envelope struct {
		Artist struct {
			Name  string `json:"name"`
			MBID  string `json:"mbid"`
			URL   string `json:"url"`
			Image []struct {
				URL  string `json:"#text"`
				Size string `json:"size"`
			} `json:"image"`
			Stats struct {
				Listeners string `json:"listeners"`
				Playcount string `json:"playcount"`
			} `json:"stats"`
			Similar struct {
				Artists []struct {
					Name string `json:"name"`
					MBID string `json:"mbid"`
					URL  string `json:"url"`
				} `json:"artist"`
			} `json:"similar"`
			Tags struct {
				Tags []struct {
					Name string `json:"name"`
				} `json:"tag"`
			} `json:"tags"`
			Bio struct {
				Published string `json:"published"`
				Summary   string `json:"summary"`
				Content   string `json:"content"`
			} `json:"bio"`
		} `json:"artist"`
		Error   int    `json:"error"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return artistdomain.NormalizedRecordV1{}, fmt.Errorf("decode Last.fm artist: %w", err)
	}
	if envelope.Error != 0 {
		return artistdomain.NormalizedRecordV1{}, fmt.Errorf("Last.fm artist: %s", envelope.Message)
	}
	source := envelope.Artist
	mbid := strings.ToLower(strings.TrimSpace(source.MBID))
	expectedMBID = strings.ToLower(strings.TrimSpace(expectedMBID))
	name := strings.TrimSpace(source.Name)
	identityMatches := mbidPattern.MatchString(mbid) && mbid == expectedMBID
	if name == "" || (!identityMatches && !artistNameMatches(name, expectedNames)) {
		return artistdomain.NormalizedRecordV1{}, fmt.Errorf("Last.fm artist does not match expected MusicBrainz identity")
	}
	record := artistdomain.NormalizedRecordV1{
		ProviderRecord: artistdomain.ProviderRecord{Provider: "lastfm", Namespace: "artist", Value: expectedMBID, PrimaryObservationID: observationID, ObservedAt: observedAt, NormalizerVersion: artistdomain.LastFMNormalizerVersion, SchemaVersion: artistdomain.NormalizedSchemaVersion},
		Names:          []artistdomain.Name{{Value: name, Type: "display", Primary: true}},
	}
	if identityMatches {
		record.IdentityCandidates = []artistdomain.IdentityCandidate{{Provider: "musicbrainz", Namespace: "artist", NormalizedValue: mbid, Confidence: 1, Evidence: "lastfm_mbid"}}
	} else {
		record.Warnings = append(record.Warnings, "Last.fm returned a mismatched MusicBrainz artist ID; contribution retained as a name-scoped aggregate")
	}
	if source.URL != "" {
		record.Links = append(record.Links, artistdomain.Link{Type: "lastfm", URL: source.URL})
	}
	bio := strings.TrimSpace(source.Bio.Content)
	if bio == "" {
		bio = strings.TrimSpace(source.Bio.Summary)
	}
	if bio != "" {
		record.Biographies = append(record.Biographies, artistdomain.Text{Value: bio, Type: "provider_biography", Markup: "html"})
	}
	for _, tag := range source.Tags.Tags {
		if name := strings.TrimSpace(tag.Name); name != "" {
			record.Tags = append(record.Tags, artistdomain.WeightedTerm{Name: name})
		}
	}
	for _, metric := range []struct{ name, raw string }{{"listeners", source.Stats.Listeners}, {"playcount", source.Stats.Playcount}} {
		if value, err := strconv.ParseFloat(metric.raw, 64); err == nil {
			record.Metrics = append(record.Metrics, artistdomain.Metric{Name: metric.name, Value: value, RawValue: metric.raw})
		}
	}
	seenImages := map[string]bool{}
	for _, image := range source.Image {
		url := strings.TrimSpace(image.URL)
		if url == "" || seenImages[url] {
			continue
		}
		seenImages[url] = true
		record.Images = append(record.Images, artistdomain.Image{SourceURL: url, Class: strings.ToLower(image.Size)})
	}
	for _, similar := range source.Similar.Artists {
		candidate := artistdomain.SimilarArtist{Name: strings.TrimSpace(similar.Name), URL: similar.URL}
		if id := strings.ToLower(strings.TrimSpace(similar.MBID)); mbidPattern.MatchString(id) {
			candidate.ProviderID = id
		}
		if candidate.Name != "" {
			record.SimilarArtists = append(record.SimilarArtists, candidate)
		}
	}
	return record, nil
}

func artistNameMatches(value string, expected []string) bool {
	value = strings.ToLower(strings.Join(strings.Fields(value), " "))
	if value == "" {
		return false
	}
	for _, candidate := range expected {
		if value == strings.ToLower(strings.Join(strings.Fields(candidate), " ")) {
			return true
		}
	}
	return false
}

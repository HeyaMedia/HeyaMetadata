package coverartarchive

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	rgdomain "github.com/HeyaMedia/HeyaMetadata/internal/domains/releasegroup"
)

func NormalizeReleaseGroup(body []byte, mbid, observationID string, observedAt time.Time) (rgdomain.NormalizedRecordV1, error) {
	mbid = strings.ToLower(strings.TrimSpace(mbid))
	if !mbidPattern.MatchString(mbid) {
		return rgdomain.NormalizedRecordV1{}, fmt.Errorf("Cover Art Archive release group requires a valid MBID")
	}
	var source struct {
		Release string `json:"release"`
		Images  []struct {
			ID       json.RawMessage   `json:"id"`
			Image    string            `json:"image"`
			Types    []string          `json:"types"`
			Front    bool              `json:"front"`
			Back     bool              `json:"back"`
			Approved bool              `json:"approved"`
			Comment  string            `json:"comment"`
			Thumbs   map[string]string `json:"thumbnails"`
		} `json:"images"`
	}
	if err := json.Unmarshal(body, &source); err != nil {
		return rgdomain.NormalizedRecordV1{}, fmt.Errorf("decode Cover Art Archive release group: %w", err)
	}
	record := rgdomain.NormalizedRecordV1{
		ProviderRecord:     rgdomain.ProviderRecord{Provider: "coverartarchive", Namespace: "release_group", Value: mbid, PrimaryObservationID: observationID, ObservedAt: observedAt, NormalizerVersion: rgdomain.CoverArtNormalizerVersion, SchemaVersion: rgdomain.NormalizedSchemaVersion},
		IdentityCandidates: []rgdomain.IdentityCandidate{{Provider: "musicbrainz", Namespace: "release_group", NormalizedValue: mbid, Confidence: 1, Evidence: "cover_art_archive_index"}},
	}
	for index, image := range source.Images {
		imageURL := normalizeImageURL(image.Image)
		if imageURL == "" {
			continue
		}
		providerID := strings.Trim(string(image.ID), `"`)
		if providerID == "" || providerID == "null" {
			providerID = fmt.Sprintf("image-%d", index+1)
		}
		classes := coverArtClasses(image.Types, image.Front, image.Back)
		for _, class := range classes {
			record.Images = append(record.Images, rgdomain.Image{ProviderImageID: providerID + ":" + class, SourceURL: imageURL, Class: class})
		}
	}
	return record, nil
}

func coverArtClasses(types []string, front, back bool) []string {
	seen := map[string]bool{}
	classes := []string{}
	appendClass := func(value string) {
		value = strings.ToLower(strings.TrimSpace(value))
		value = strings.NewReplacer(" ", "_", "-", "_").Replace(value)
		switch value {
		case "front":
			value = "cover"
		case "back":
			value = "back_cover"
		case "medium":
			value = "disc"
		case "":
			return
		}
		if !seen[value] {
			seen[value] = true
			classes = append(classes, value)
		}
	}
	for _, value := range types {
		appendClass(value)
	}
	if front {
		appendClass("front")
	}
	if back {
		appendClass("back")
	}
	if len(classes) == 0 {
		classes = append(classes, "artwork")
	}
	return classes
}

func normalizeImageURL(value string) string {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return ""
	}
	parsed.Scheme = "https"
	return parsed.String()
}

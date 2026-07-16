package tidal

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	artistdomain "github.com/HeyaMedia/HeyaMetadata/internal/domains/artist"
)

func NormalizeArtist(body []byte, expectedID, observationID string, observedAt time.Time) (artistdomain.NormalizedRecordV1, error) {
	var envelope struct {
		Data struct {
			ID         string `json:"id"`
			Type       string `json:"type"`
			Attributes struct {
				Name          string  `json:"name"`
				Popularity    float64 `json:"popularity"`
				ExternalLinks []struct {
					Href string `json:"href"`
					Meta struct {
						Type string `json:"type"`
					} `json:"meta"`
				} `json:"externalLinks"`
			} `json:"attributes"`
		} `json:"data"`
		Included []struct {
			ID         string `json:"id"`
			Type       string `json:"type"`
			Attributes struct {
				MediaType string `json:"mediaType"`
				Files     []struct {
					Href string `json:"href"`
					Meta struct {
						Width  int `json:"width"`
						Height int `json:"height"`
					} `json:"meta"`
				} `json:"files"`
				Name          string  `json:"name"`
				Popularity    float64 `json:"popularity"`
				ExternalLinks []struct {
					Href string `json:"href"`
					Meta struct {
						Type string `json:"type"`
					} `json:"meta"`
				} `json:"externalLinks"`
			} `json:"attributes"`
		} `json:"included"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return artistdomain.NormalizedRecordV1{}, fmt.Errorf("decode Tidal artist: %w", err)
	}
	expectedID = strings.TrimSpace(expectedID)
	id := strings.TrimSpace(envelope.Data.ID)
	name := strings.TrimSpace(envelope.Data.Attributes.Name)
	if envelope.Data.Type != "artists" || id == "" || id != expectedID || name == "" {
		return artistdomain.NormalizedRecordV1{}, fmt.Errorf("Tidal artist does not match the expected identity")
	}
	record := artistdomain.NormalizedRecordV1{
		ProviderRecord:     artistdomain.ProviderRecord{Provider: "tidal", Namespace: "artist", Value: id, PrimaryObservationID: observationID, ObservedAt: observedAt, NormalizerVersion: artistdomain.TidalNormalizerVersion, SchemaVersion: artistdomain.NormalizedSchemaVersion},
		IdentityCandidates: []artistdomain.IdentityCandidate{{Provider: "tidal", Namespace: "artist", NormalizedValue: id, Confidence: 1, Evidence: "provider_record"}},
		Names:              []artistdomain.Name{{Value: name, Type: "display", Primary: true}},
	}
	if popularity := envelope.Data.Attributes.Popularity; popularity > 0 {
		record.Metrics = append(record.Metrics, artistdomain.Metric{Name: "popularity", Value: popularity})
	}
	for _, link := range envelope.Data.Attributes.ExternalLinks {
		if link.Meta.Type == "TIDAL_SHARING" && strings.HasPrefix(link.Href, "https://") {
			record.Links = append(record.Links, artistdomain.Link{Type: "tidal", URL: link.Href})
			break
		}
	}
	for _, included := range envelope.Included {
		switch {
		case included.Type == "artworks" && included.Attributes.MediaType == "IMAGE":
			best := struct {
				href          string
				width, height int
			}{}
			for _, file := range included.Attributes.Files {
				if strings.HasPrefix(file.Href, "https://") && file.Meta.Width > best.width {
					best.href, best.width, best.height = file.Href, file.Meta.Width, file.Meta.Height
				}
			}
			if best.href != "" {
				record.Images = append(record.Images, artistdomain.Image{ProviderImageID: included.ID, SourceURL: best.href, Class: "profile", Width: best.width, Height: best.height})
			}
		case included.Type == "artists":
			name := strings.TrimSpace(included.Attributes.Name)
			if included.ID == "" || included.ID == id || name == "" {
				continue
			}
			similar := artistdomain.SimilarArtist{ProviderID: included.ID, Name: name, Score: included.Attributes.Popularity}
			for _, link := range included.Attributes.ExternalLinks {
				if link.Meta.Type == "TIDAL_SHARING" && strings.HasPrefix(link.Href, "https://") {
					similar.URL = link.Href
					break
				}
			}
			record.SimilarArtists = append(record.SimilarArtists, similar)
		}
	}
	return record, nil
}

package discogs

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	artistdomain "github.com/HeyaMedia/HeyaMetadata/internal/domains/artist"
)

type artistRef struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Active *bool  `json:"active"`
}

func NormalizeArtist(body []byte, observationID string, observedAt time.Time) (artistdomain.NormalizedRecordV1, error) {
	var source struct {
		ID             int64       `json:"id"`
		Name           string      `json:"name"`
		RealName       string      `json:"realname"`
		Profile        string      `json:"profile"`
		NameVariations []string    `json:"namevariations"`
		Aliases        []artistRef `json:"aliases"`
		Members        []artistRef `json:"members"`
		Groups         []artistRef `json:"groups"`
		URLs           []string    `json:"urls"`
		DataQuality    string      `json:"data_quality"`
		Images         []struct {
			Type        string `json:"type"`
			URI         string `json:"uri"`
			ResourceURL string `json:"resource_url"`
			Width       int    `json:"width"`
			Height      int    `json:"height"`
		} `json:"images"`
	}
	if err := json.Unmarshal(body, &source); err != nil {
		return artistdomain.NormalizedRecordV1{}, fmt.Errorf("decode Discogs artist: %w", err)
	}
	name, id := strings.TrimSpace(source.Name), strconv.FormatInt(source.ID, 10)
	if source.ID < 1 || name == "" {
		return artistdomain.NormalizedRecordV1{}, fmt.Errorf("Discogs artist is missing identity or name")
	}
	record := artistdomain.NormalizedRecordV1{
		ProviderRecord:     artistdomain.ProviderRecord{Provider: "discogs", Namespace: "artist", Value: id, PrimaryObservationID: observationID, ObservedAt: observedAt, NormalizerVersion: artistdomain.DiscogsNormalizerVersion, SchemaVersion: artistdomain.NormalizedSchemaVersion},
		IdentityCandidates: []artistdomain.IdentityCandidate{{Provider: "discogs", Namespace: "artist", NormalizedValue: id, Confidence: 1, Evidence: "provider_record"}},
		Names:              []artistdomain.Name{{Value: name, Type: "display", Primary: true}},
	}
	if value := strings.TrimSpace(source.RealName); value != "" && value != name {
		record.Names = append(record.Names, artistdomain.Name{Value: value, Type: "legal_name"})
	}
	for _, value := range source.NameVariations {
		if value = strings.TrimSpace(value); value != "" {
			record.Names = append(record.Names, artistdomain.Name{Value: value, Type: "name_variation"})
		}
	}
	if value := strings.TrimSpace(source.Profile); value != "" {
		record.Biographies = append(record.Biographies, artistdomain.Text{Value: value, Type: "provider_profile", Markup: "discogs"})
	}
	appendRelations := func(kind string, refs []artistRef) {
		for _, ref := range refs {
			if ref.ID < 1 {
				continue
			}
			record.Relationships = append(record.Relationships, artistdomain.Relationship{Type: kind, TargetProvider: "discogs", TargetNamespace: "artist", TargetID: strconv.FormatInt(ref.ID, 10), TargetName: strings.TrimSpace(ref.Name), Ended: inactiveAsEnded(ref.Active)})
		}
	}
	// Discogs aliases are distinct artist records, not safe identity aliases.
	appendRelations("discogs_alias", source.Aliases)
	appendRelations("member", source.Members)
	appendRelations("group", source.Groups)
	for _, value := range source.URLs {
		if value = strings.TrimSpace(value); value != "" {
			record.Links = append(record.Links, artistdomain.Link{Type: "external", URL: value})
		}
	}
	for i, image := range source.Images {
		url := strings.TrimSpace(image.ResourceURL)
		if url == "" {
			url = strings.TrimSpace(image.URI)
		}
		if url == "" {
			continue
		}
		record.Images = append(record.Images, artistdomain.Image{ProviderImageID: strconv.Itoa(i), SourceURL: url, Class: strings.ToLower(strings.TrimSpace(image.Type)), Width: image.Width, Height: image.Height})
	}
	if quality := strings.TrimSpace(source.DataQuality); quality != "" {
		record.Annotations = append(record.Annotations, artistdomain.Text{Value: quality, Type: "data_quality"})
	}
	return record, nil
}

func inactiveAsEnded(active *bool) *bool {
	if active == nil {
		return nil
	}
	ended := !*active
	return &ended
}

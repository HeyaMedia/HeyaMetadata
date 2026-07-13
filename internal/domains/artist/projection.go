package artist

import (
	"sort"
	"strings"
	"time"
)

type ExternalID struct {
	Provider  string `json:"provider"`
	Namespace string `json:"namespace"`
	Value     string `json:"value"`
}
type Display struct {
	Name           string `json:"name"`
	Disambiguation string `json:"disambiguation,omitempty"`
	ImageID        string `json:"image_id,omitempty"`
}
type Provenance struct {
	Provider           string `json:"provider"`
	NormalizedRecordID string `json:"normalized_record_id"`
	ObservationID      string `json:"observation_id"`
}
type ProviderFreshness struct {
	State             string    `json:"state"`
	LastSuccessAt     time.Time `json:"last_success_at"`
	LastObservationID string    `json:"last_observation_id"`
}
type Freshness struct {
	State      string                       `json:"state"`
	UpdatedAt  time.Time                    `json:"updated_at"`
	FreshUntil time.Time                    `json:"fresh_until"`
	Providers  map[string]ProviderFreshness `json:"providers"`
}
type ProjectedImage struct {
	ID            string  `json:"id"`
	Class         string  `json:"class"`
	Language      string  `json:"language,omitempty"`
	Width         int     `json:"width,omitempty"`
	Height        int     `json:"height,omitempty"`
	ProviderScore float64 `json:"provider_score,omitempty"`
	Provider      string  `json:"provider"`
}
type ProjectedTerm struct {
	Name       string  `json:"name"`
	Weight     float64 `json:"weight,omitempty"`
	Provider   string  `json:"provider"`
	ProviderID string  `json:"provider_id,omitempty"`
}
type ProjectedMetric struct {
	Name     string  `json:"name"`
	Value    float64 `json:"value"`
	RawValue string  `json:"raw_value,omitempty"`
	Provider string  `json:"provider"`
}
type ProjectedRelationship struct {
	Relationship
	Provider string `json:"provider"`
}
type ProjectedSimilarArtist struct {
	SimilarArtist
	Provider string `json:"provider"`
}

type DetailData struct {
	Names          []Name                   `json:"names"`
	Classification Classification           `json:"classification"`
	Lifecycle      Lifecycle                `json:"lifecycle"`
	Areas          []Area                   `json:"areas"`
	Biographies    []Text                   `json:"biographies"`
	Annotations    []Text                   `json:"annotations"`
	Genres         []ProjectedTerm          `json:"genres"`
	Tags           []ProjectedTerm          `json:"tags"`
	Links          []Link                   `json:"links"`
	Images         []ProjectedImage         `json:"images"`
	Metrics        []ProjectedMetric        `json:"metrics"`
	Relationships  []ProjectedRelationship  `json:"relationships"`
	SimilarArtists []ProjectedSimilarArtist `json:"similar_artists"`
}
type DetailDocument struct {
	SchemaVersion     int                     `json:"schema_version"`
	ProjectionVersion int64                   `json:"projection_version"`
	ID                string                  `json:"id"`
	Kind              string                  `json:"kind"`
	Slug              string                  `json:"slug"`
	Display           Display                 `json:"display"`
	ExternalIDs       []ExternalID            `json:"external_ids"`
	Data              DetailData              `json:"data"`
	Freshness         Freshness               `json:"freshness"`
	Provenance        map[string][]Provenance `json:"provenance"`
}
type SummaryDocument struct {
	SchemaVersion     int          `json:"schema_version"`
	ProjectionVersion int64        `json:"projection_version"`
	ID                string       `json:"id"`
	Kind              string       `json:"kind"`
	Slug              string       `json:"slug"`
	Display           Display      `json:"display"`
	ArtistType        string       `json:"artist_type,omitempty"`
	Genres            []string     `json:"genres"`
	ExternalIDs       []ExternalID `json:"external_ids"`
	Freshness         Freshness    `json:"freshness"`
}
type Projection struct {
	Detail      DetailDocument
	Summary     SummaryDocument
	SearchNames []Name
}

func Combine(entityID, slug string, projectionVersion int64, records []RecordInput, imageIDs map[string]string, now time.Time) Projection {
	sort.Slice(records, func(i, j int) bool {
		a, b := artistProviderPriority(records[i].Record.ProviderRecord.Provider), artistProviderPriority(records[j].Record.ProviderRecord.Provider)
		if a != b {
			return a < b
		}
		return records[i].ID < records[j].ID
	})
	detail := DetailDocument{SchemaVersion: ProjectionSchemaVersion, ProjectionVersion: projectionVersion, ID: entityID, Kind: "artist", Slug: slug, Freshness: Freshness{State: "fresh", UpdatedAt: now, FreshUntil: now.Add(7 * 24 * time.Hour), Providers: map[string]ProviderFreshness{}}, Provenance: map[string][]Provenance{}}
	seen := map[string]bool{}
	for _, input := range records {
		record, provider := input.Record, input.Record.ProviderRecord.Provider
		p := Provenance{Provider: provider, NormalizedRecordID: input.ID, ObservationID: record.ProviderRecord.PrimaryObservationID}
		detail.Freshness.Providers[provider] = ProviderFreshness{State: "fresh", LastSuccessAt: record.ProviderRecord.ObservedAt, LastObservationID: record.ProviderRecord.PrimaryObservationID}
		for _, value := range record.IdentityCandidates {
			key := "id:" + value.Provider + ":" + value.Namespace + ":" + value.NormalizedValue
			if value.NormalizedValue != "" && !seen[key] {
				seen[key] = true
				detail.ExternalIDs = append(detail.ExternalIDs, ExternalID{value.Provider, value.Namespace, value.NormalizedValue})
				addArtistProvenance(detail.Provenance, "identity.external_ids", p)
			}
		}
		for _, value := range record.Names {
			key := "name:" + strings.ToLower(value.Value) + ":" + value.Language + ":" + value.Type
			if value.Value != "" && !seen[key] {
				seen[key] = true
				detail.Data.Names = append(detail.Data.Names, value)
			}
			if detail.Display.Name == "" && value.Primary {
				detail.Display.Name = value.Value
				addArtistProvenance(detail.Provenance, "display.name", p)
			}
		}
		if detail.Display.Disambiguation == "" && record.Disambiguation != "" {
			detail.Display.Disambiguation = record.Disambiguation
			addArtistProvenance(detail.Provenance, "display.disambiguation", p)
		}
		if detail.Data.Classification.ArtistType == "" && record.Classification.ArtistType != "" {
			detail.Data.Classification.ArtistType = record.Classification.ArtistType
			addArtistProvenance(detail.Provenance, "data.classification.artist_type", p)
		}
		if detail.Data.Classification.Gender == "" && record.Classification.Gender != "" {
			detail.Data.Classification.Gender = record.Classification.Gender
			addArtistProvenance(detail.Provenance, "data.classification.gender", p)
		}
		if detail.Data.Lifecycle.Ended == nil && record.Lifecycle.Ended != nil {
			detail.Data.Lifecycle.Ended = record.Lifecycle.Ended
		}
		for _, value := range record.Lifecycle.Dates {
			key := "date:" + value.Type + ":" + value.Value + ":" + value.Precision
			if !seen[key] {
				seen[key] = true
				detail.Data.Lifecycle.Dates = append(detail.Data.Lifecycle.Dates, value)
				addArtistProvenance(detail.Provenance, "data.lifecycle", p)
			}
		}
		for _, value := range record.Areas {
			key := "area:" + value.Role + ":" + value.ProviderID + ":" + strings.ToLower(value.Name)
			if !seen[key] {
				seen[key] = true
				detail.Data.Areas = append(detail.Data.Areas, value)
			}
		}
		appendTexts := func(scope string, values []Text, target *[]Text) {
			for _, value := range values {
				key := scope + ":" + value.Language + ":" + value.Type + ":" + value.Value
				if value.Value != "" && !seen[key] {
					seen[key] = true
					*target = append(*target, value)
					addArtistProvenance(detail.Provenance, "data."+scope, p)
				}
			}
		}
		appendTexts("biographies", record.Biographies, &detail.Data.Biographies)
		appendTexts("annotations", record.Annotations, &detail.Data.Annotations)
		for _, value := range record.Genres {
			key := "genre:" + provider + ":" + strings.ToLower(value.Name)
			if value.Name != "" && !seen[key] {
				seen[key] = true
				detail.Data.Genres = append(detail.Data.Genres, ProjectedTerm{Name: value.Name, Weight: value.Weight, Provider: provider, ProviderID: value.ProviderID})
			}
		}
		for _, value := range record.Tags {
			key := "tag:" + provider + ":" + strings.ToLower(value.Name)
			if value.Name != "" && !seen[key] {
				seen[key] = true
				detail.Data.Tags = append(detail.Data.Tags, ProjectedTerm{Name: value.Name, Weight: value.Weight, Provider: provider, ProviderID: value.ProviderID})
			}
		}
		for _, value := range record.Links {
			key := "link:" + value.Type + ":" + value.URL
			if value.URL != "" && !seen[key] {
				seen[key] = true
				detail.Data.Links = append(detail.Data.Links, value)
			}
		}
		for _, value := range record.Images {
			key := ImageKey(provider, value)
			imageID := imageIDs[key]
			if imageID == "" {
				continue
			}
			if !seen["image:"+key] {
				seen["image:"+key] = true
				projected := ProjectedImage{ID: imageID, Class: value.Class, Language: value.Language, Width: value.Width, Height: value.Height, ProviderScore: value.ProviderScore, Provider: provider}
				detail.Data.Images = append(detail.Data.Images, projected)
				if detail.Display.ImageID == "" && projected.ID != "" {
					detail.Display.ImageID = projected.ID
					addArtistProvenance(detail.Provenance, "display.image_id", p)
				}
			}
		}
		for _, value := range record.Metrics {
			key := "metric:" + provider + ":" + value.Name
			if !seen[key] {
				seen[key] = true
				detail.Data.Metrics = append(detail.Data.Metrics, ProjectedMetric{Name: value.Name, Value: value.Value, RawValue: value.RawValue, Provider: provider})
			}
		}
		for _, value := range record.Relationships {
			key := "rel:" + provider + ":" + value.Type + ":" + value.TargetProvider + ":" + value.TargetNamespace + ":" + value.TargetID
			if !seen[key] {
				seen[key] = true
				detail.Data.Relationships = append(detail.Data.Relationships, ProjectedRelationship{Relationship: value, Provider: provider})
			}
		}
		for _, value := range record.SimilarArtists {
			key := "similar:" + provider + ":" + value.ProviderID + ":" + strings.ToLower(value.Name)
			if !seen[key] {
				seen[key] = true
				detail.Data.SimilarArtists = append(detail.Data.SimilarArtists, ProjectedSimilarArtist{SimilarArtist: value, Provider: provider})
			}
		}
	}
	if detail.Display.Name == "" && len(detail.Data.Names) > 0 {
		detail.Display.Name = detail.Data.Names[0].Value
	}
	sort.Slice(detail.ExternalIDs, func(i, j int) bool {
		return detail.ExternalIDs[i].Provider+detail.ExternalIDs[i].Namespace+detail.ExternalIDs[i].Value < detail.ExternalIDs[j].Provider+detail.ExternalIDs[j].Namespace+detail.ExternalIDs[j].Value
	})
	genres := []string{}
	genreSeen := map[string]bool{}
	for _, value := range detail.Data.Genres {
		key := strings.ToLower(value.Name)
		if !genreSeen[key] {
			genreSeen[key] = true
			genres = append(genres, value.Name)
		}
	}
	summary := SummaryDocument{SchemaVersion: ProjectionSchemaVersion, ProjectionVersion: projectionVersion, ID: entityID, Kind: "artist", Slug: slug, Display: detail.Display, ArtistType: detail.Data.Classification.ArtistType, Genres: genres, ExternalIDs: detail.ExternalIDs, Freshness: detail.Freshness}
	return Projection{Detail: detail, Summary: summary, SearchNames: detail.Data.Names}
}

func ImageKey(provider string, image Image) string {
	return provider + ":" + image.Class + ":" + image.ProviderImageID
}
func artistProviderPriority(provider string) int {
	switch provider {
	case "musicbrainz":
		return 10
	case "wikidata":
		return 20
	case "discogs":
		return 30
	case "apple":
		return 40
	case "deezer":
		return 50
	case "lastfm":
		return 60
	}
	return 100
}
func addArtistProvenance(target map[string][]Provenance, scope string, value Provenance) {
	for _, existing := range target[scope] {
		if existing == value {
			return
		}
	}
	target[scope] = append(target[scope], value)
}

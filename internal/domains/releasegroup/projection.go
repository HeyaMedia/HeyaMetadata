package releasegroup

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/textmatch"
)

type ExternalID struct {
	Provider  string `json:"provider"`
	Namespace string `json:"namespace"`
	Value     string `json:"value"`
}
type Display struct {
	Title          string `json:"title"`
	ArtistCredit   string `json:"artist_credit,omitempty"`
	Year           int    `json:"year,omitempty"`
	ImageID        string `json:"image_id,omitempty"`
	Disambiguation string `json:"disambiguation,omitempty"`
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
type ProjectedTerm struct {
	WeightedTerm
	Provider string `json:"provider"`
}
type ProjectedRating struct {
	Rating
	Provider string `json:"provider"`
}
type ProjectedMetric struct {
	Metric
	Provider string `json:"provider"`
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
type ProjectedEdition struct {
	Provider   string          `json:"provider"`
	Namespace  string          `json:"namespace"`
	ProviderID string          `json:"provider_id"`
	Title      string          `json:"title"`
	Status     string          `json:"status,omitempty"`
	Date       DateValue       `json:"date"`
	Country    string          `json:"country,omitempty"`
	Barcode    string          `json:"barcode,omitempty"`
	TrackCount int             `json:"track_count,omitempty"`
	DurationMS int64           `json:"duration_ms,omitempty"`
	Explicit   *bool           `json:"explicit,omitempty"`
	Formats    []string        `json:"formats"`
	Link       string          `json:"link,omitempty"`
	ImageID    string          `json:"image_id,omitempty"`
	Sources    []EditionSource `json:"sources"`
}
type EditionSource struct {
	Provider   string `json:"provider"`
	Namespace  string `json:"namespace"`
	ProviderID string `json:"provider_id"`
	Link       string `json:"link,omitempty"`
}
type ProjectedTrack struct {
	Track
	Provider          string `json:"provider"`
	RecordingEntityID string `json:"recording_entity_id,omitempty"`
}
type DetailData struct {
	Titles         []Title            `json:"titles"`
	ArtistCredits  []ArtistCredit     `json:"artist_credits"`
	Classification Classification     `json:"classification"`
	Dates          []DateValue        `json:"dates"`
	Descriptions   []Text             `json:"descriptions"`
	Annotations    []Text             `json:"annotations"`
	Genres         []ProjectedTerm    `json:"genres"`
	Tags           []ProjectedTerm    `json:"tags"`
	Ratings        []ProjectedRating  `json:"ratings"`
	Links          []Link             `json:"links"`
	Images         []ProjectedImage   `json:"images"`
	Metrics        []ProjectedMetric  `json:"metrics"`
	Editions       []ProjectedEdition `json:"editions"`
	Tracks         []ProjectedTrack   `json:"tracks"`
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
	PrimaryType       string       `json:"primary_type,omitempty"`
	SecondaryTypes    []string     `json:"secondary_types"`
	Genres            []string     `json:"genres"`
	ExternalIDs       []ExternalID `json:"external_ids"`
	Freshness         Freshness    `json:"freshness"`
}
type Projection struct {
	Detail       DetailDocument
	Summary      SummaryDocument
	SearchTitles []Title
}

func Combine(entityID, slug string, version int64, records []RecordInput, imageIDs map[string]string, now time.Time) Projection {
	sort.Slice(records, func(i, j int) bool {
		a, b := releaseGroupProviderPriority(records[i].Record.ProviderRecord.Provider), releaseGroupProviderPriority(records[j].Record.ProviderRecord.Provider)
		if a != b {
			return a < b
		}
		return records[i].ID < records[j].ID
	})
	detail := DetailDocument{SchemaVersion: ProjectionSchemaVersion, ProjectionVersion: version, ID: entityID, Kind: "release_group", Slug: slug, Freshness: Freshness{State: "fresh", UpdatedAt: now, FreshUntil: now.Add(7 * 24 * time.Hour), Providers: map[string]ProviderFreshness{}}, Provenance: map[string][]Provenance{}}
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
				addReleaseGroupProvenance(detail.Provenance, "identity.external_ids", p)
			}
		}
		for _, value := range record.Titles {
			key := "title:" + strings.ToLower(value.Value) + ":" + value.Language + ":" + value.Type
			if value.Value != "" && !seen[key] {
				seen[key] = true
				detail.Data.Titles = append(detail.Data.Titles, value)
			}
			if detail.Display.Title == "" && value.Primary {
				detail.Display.Title = value.Value
				addReleaseGroupProvenance(detail.Provenance, "display.title", p)
			}
		}
		if detail.Display.Disambiguation == "" && record.Disambiguation != "" {
			detail.Display.Disambiguation = record.Disambiguation
		}
		if len(detail.Data.ArtistCredits) == 0 && len(record.ArtistCredits) > 0 {
			detail.Data.ArtistCredits = append([]ArtistCredit(nil), record.ArtistCredits...)
			detail.Display.ArtistCredit = formatArtistCredit(record.ArtistCredits)
			addReleaseGroupProvenance(detail.Provenance, "display.artist_credit", p)
		}
		if detail.Data.Classification.PrimaryType == "" && record.Classification.PrimaryType != "" {
			detail.Data.Classification.PrimaryType = record.Classification.PrimaryType
		}
		detail.Data.Classification.SecondaryTypes = unionReleaseGroupStrings(detail.Data.Classification.SecondaryTypes, record.Classification.SecondaryTypes)
		for _, value := range record.Dates {
			key := "date:" + value.Type + ":" + value.Value + ":" + value.Precision
			if value.Value != "" && !seen[key] {
				seen[key] = true
				detail.Data.Dates = append(detail.Data.Dates, value)
			}
		}
		appendTexts := func(scope string, values []Text, target *[]Text) {
			for _, value := range values {
				key := scope + ":" + value.Language + ":" + value.Type + ":" + value.Value
				if value.Value != "" && !seen[key] {
					seen[key] = true
					*target = append(*target, value)
				}
			}
		}
		appendTexts("description", record.Descriptions, &detail.Data.Descriptions)
		appendTexts("annotation", record.Annotations, &detail.Data.Annotations)
		for _, value := range record.Genres {
			key := "genre:" + provider + ":" + strings.ToLower(value.Name)
			if value.Name != "" && !seen[key] {
				seen[key] = true
				detail.Data.Genres = append(detail.Data.Genres, ProjectedTerm{WeightedTerm: value, Provider: provider})
			}
		}
		for _, value := range record.Tags {
			key := "tag:" + provider + ":" + strings.ToLower(value.Name)
			if value.Name != "" && !seen[key] {
				seen[key] = true
				detail.Data.Tags = append(detail.Data.Tags, ProjectedTerm{WeightedTerm: value, Provider: provider})
			}
		}
		for _, value := range record.Ratings {
			key := "rating:" + provider + ":" + value.System
			if !seen[key] {
				seen[key] = true
				detail.Data.Ratings = append(detail.Data.Ratings, ProjectedRating{Rating: value, Provider: provider})
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
			if !seen["image:"+key] {
				seen["image:"+key] = true
				projected := ProjectedImage{ID: imageIDs[key], Class: value.Class, Language: value.Language, Width: value.Width, Height: value.Height, ProviderScore: value.ProviderScore, Provider: provider}
				detail.Data.Images = append(detail.Data.Images, projected)
				if detail.Display.ImageID == "" && projected.ID != "" {
					detail.Display.ImageID = projected.ID
				}
			}
		}
		for _, value := range record.Metrics {
			key := "metric:" + provider + ":" + value.Name
			if !seen[key] {
				seen[key] = true
				detail.Data.Metrics = append(detail.Data.Metrics, ProjectedMetric{Metric: value, Provider: provider})
			}
		}
		for _, value := range record.Editions {
			key := "edition:" + value.Provider + ":" + value.Namespace + ":" + value.ProviderID
			if seen[key] {
				continue
			}
			seen[key] = true
			projected := ProjectedEdition{Provider: value.Provider, Namespace: value.Namespace, ProviderID: value.ProviderID, Title: value.Title, Status: value.Status, Date: value.Date, Country: value.Country, Barcode: value.Barcode, TrackCount: value.TrackCount, DurationMS: value.DurationMS, Explicit: value.Explicit, Formats: value.Formats, Link: value.Link, Sources: []EditionSource{{Provider: value.Provider, Namespace: value.Namespace, ProviderID: value.ProviderID, Link: value.Link}}}
			if value.Image != nil {
				projected.ImageID = imageIDs[ImageKey(provider, *value.Image)]
			}
			if index := equivalentEditionIndex(detail.Data.Editions, projected); index >= 0 {
				existing := &detail.Data.Editions[index]
				existing.Sources = append(existing.Sources, projected.Sources...)
				existing.Formats = unionReleaseGroupStrings(existing.Formats, projected.Formats)
				if existing.ImageID == "" {
					existing.ImageID = projected.ImageID
				}
				if existing.Barcode == "" {
					existing.Barcode = projected.Barcode
				}
				if existing.TrackCount == 0 {
					existing.TrackCount = projected.TrackCount
				}
				if existing.DurationMS == 0 {
					existing.DurationMS = projected.DurationMS
				}
			} else {
				detail.Data.Editions = append(detail.Data.Editions, projected)
			}
		}
		for _, value := range record.Tracks {
			key := "track:" + provider + ":" + value.ProviderID + ":" + value.Position + ":" + strings.ToLower(value.Title)
			if !seen[key] {
				seen[key] = true
				detail.Data.Tracks = append(detail.Data.Tracks, ProjectedTrack{Track: value, Provider: provider})
			}
		}
	}
	if detail.Display.Title == "" && len(detail.Data.Titles) > 0 {
		detail.Display.Title = detail.Data.Titles[0].Value
	}
	detail.Display.Year = firstReleaseYear(detail.Data.Dates)
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
	summary := SummaryDocument{SchemaVersion: ProjectionSchemaVersion, ProjectionVersion: version, ID: entityID, Kind: "release_group", Slug: slug, Display: detail.Display, PrimaryType: detail.Data.Classification.PrimaryType, SecondaryTypes: detail.Data.Classification.SecondaryTypes, Genres: genres, ExternalIDs: detail.ExternalIDs, Freshness: detail.Freshness}
	return Projection{Detail: detail, Summary: summary, SearchTitles: detail.Data.Titles}
}

func equivalentEditionIndex(values []ProjectedEdition, incoming ProjectedEdition) int {
	for i, existing := range values {
		if existing.Provider == incoming.Provider {
			continue
		}
		if existing.Barcode != "" && incoming.Barcode != "" && existing.Barcode == incoming.Barcode {
			return i
		}
		leftYear, rightYear := dateYear(existing.Date.Value), dateYear(incoming.Date.Value)
		if leftYear > 0 && rightYear > 0 && leftYear != rightYear {
			continue
		}
		if existing.TrackCount > 0 && incoming.TrackCount > 0 && existing.TrackCount != incoming.TrackCount {
			continue
		}
		if textmatch.EquivalentRelease(existing.Title, leftYear, incoming.Title, rightYear) {
			return i
		}
	}
	return -1
}
func dateYear(value string) int {
	if len(value) < 4 {
		return 0
	}
	year, _ := strconv.Atoi(value[:4])
	return year
}
func ImageKey(provider string, image Image) string {
	return provider + ":" + image.Class + ":" + image.ProviderImageID
}
func releaseGroupProviderPriority(provider string) int {
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
func formatArtistCredit(credits []ArtistCredit) string {
	var out strings.Builder
	for _, credit := range credits {
		out.WriteString(credit.Name)
		out.WriteString(credit.JoinPhrase)
	}
	return out.String()
}
func unionReleaseGroupStrings(existing, incoming []string) []string {
	seen := map[string]bool{}
	for _, value := range existing {
		seen[strings.ToLower(value)] = true
	}
	for _, value := range incoming {
		key := strings.ToLower(value)
		if value != "" && !seen[key] {
			seen[key] = true
			existing = append(existing, value)
		}
	}
	return existing
}
func addReleaseGroupProvenance(target map[string][]Provenance, scope string, value Provenance) {
	for _, existing := range target[scope] {
		if existing == value {
			return
		}
	}
	target[scope] = append(target[scope], value)
}
func firstReleaseYear(dates []DateValue) int {
	result := 0
	for _, date := range dates {
		if len(date.Value) < 4 {
			continue
		}
		year := 0
		for _, char := range date.Value[:4] {
			if char < '0' || char > '9' {
				year = 0
				break
			}
			year = year*10 + int(char-'0')
		}
		if year > 0 && (result == 0 || year < result) {
			result = year
		}
	}
	return result
}

// Package releasegroup models work-level music releases independently from editions.
package releasegroup

import "time"

const (
	NormalizedSchemaVersion      = 1
	MusicBrainzNormalizerVersion = "musicbrainz-release-group/v1"
	WikidataNormalizerVersion    = "wikidata-release-group/v1"
	DiscogsNormalizerVersion     = "discogs-master/v1"
	CoverArtNormalizerVersion    = "cover-art-archive-release-group/v1"
	FanartNormalizerVersion      = "fanart-music-release-group/v1"
	AppleNormalizerVersion       = "apple-album/v1"
	DeezerNormalizerVersion      = "deezer-album/v1"
	LastFMNormalizerVersion      = "lastfm-album/v1"
	MergeVersion                 = "release-group-merge/v1"
	ProjectionSchemaVersion      = 1
)

type ProviderRecord struct {
	Provider                 string    `json:"provider"`
	Namespace                string    `json:"namespace"`
	Value                    string    `json:"value"`
	PrimaryObservationID     string    `json:"primary_observation_id"`
	SupportingObservationIDs []string  `json:"supporting_observation_ids,omitempty"`
	ObservedAt               time.Time `json:"observed_at"`
	NormalizerVersion        string    `json:"normalizer_version"`
	SchemaVersion            int       `json:"schema_version"`
}
type IdentityCandidate struct {
	Provider        string  `json:"provider"`
	Namespace       string  `json:"namespace"`
	NormalizedValue string  `json:"normalized_value"`
	Confidence      float64 `json:"confidence"`
	Evidence        string  `json:"evidence"`
}
type Title struct {
	Value     string `json:"value"`
	SortValue string `json:"sort_value,omitempty"`
	Language  string `json:"language,omitempty"`
	Type      string `json:"type"`
	Primary   bool   `json:"primary,omitempty"`
}
type ArtistCredit struct {
	Position        int    `json:"position"`
	Name            string `json:"name"`
	JoinPhrase      string `json:"join_phrase,omitempty"`
	ArtistEntityID  string `json:"artist_entity_id,omitempty" format:"uuid"`
	ArtistProvider  string `json:"artist_provider"`
	ArtistNamespace string `json:"artist_namespace"`
	ArtistID        string `json:"artist_id"`
	ArtistName      string `json:"artist_name"`
	ResolutionState string `json:"resolution_state,omitempty" enum:"materialized,unresolved"`
}
type Classification struct {
	PrimaryType    string   `json:"primary_type,omitempty"`
	SecondaryTypes []string `json:"secondary_types"`
}
type DateValue struct {
	Value     string `json:"value"`
	Precision string `json:"precision"`
	Type      string `json:"type"`
}
type WeightedTerm struct {
	ProviderID string  `json:"provider_id,omitempty"`
	Name       string  `json:"name"`
	Weight     float64 `json:"weight,omitempty"`
}
type Rating struct {
	System   string  `json:"system"`
	Value    float64 `json:"value"`
	ScaleMin float64 `json:"scale_min"`
	ScaleMax float64 `json:"scale_max"`
	Votes    int64   `json:"votes,omitempty"`
	RawValue string  `json:"raw_value,omitempty"`
}
type Text struct {
	Value    string `json:"value"`
	Language string `json:"language,omitempty"`
	Type     string `json:"type"`
	Markup   string `json:"markup,omitempty"`
}
type Link struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}
type Image struct {
	ProviderImageID string  `json:"provider_image_id"`
	SourceURL       string  `json:"source_url"`
	Class           string  `json:"class"`
	Language        string  `json:"language,omitempty"`
	Width           int     `json:"width,omitempty"`
	Height          int     `json:"height,omitempty"`
	ProviderScore   float64 `json:"provider_score,omitempty"`
}
type Metric struct {
	Name     string  `json:"name"`
	Value    float64 `json:"value"`
	RawValue string  `json:"raw_value,omitempty"`
}
type Edition struct {
	Provider   string    `json:"provider"`
	Namespace  string    `json:"namespace"`
	ProviderID string    `json:"provider_id"`
	Title      string    `json:"title"`
	Status     string    `json:"status,omitempty"`
	Date       DateValue `json:"date"`
	Country    string    `json:"country,omitempty"`
	Barcode    string    `json:"barcode,omitempty"`
	TrackCount int       `json:"track_count,omitempty"`
	DurationMS int64     `json:"duration_ms,omitempty"`
	Explicit   *bool     `json:"explicit,omitempty"`
	Formats    []string  `json:"formats"`
	Link       string    `json:"link,omitempty"`
	Image      *Image    `json:"image,omitempty"`
}
type Track struct {
	ProviderID        string         `json:"provider_id,omitempty"`
	Position          string         `json:"position"`
	Number            int            `json:"number,omitempty"`
	DiscNumber        int            `json:"disc_number,omitempty"`
	Title             string         `json:"title"`
	DurationMS        int64          `json:"duration_ms,omitempty"`
	ISRC              string         `json:"isrc,omitempty"`
	RecordingProvider string         `json:"recording_provider,omitempty"`
	RecordingID       string         `json:"recording_id,omitempty"`
	ArtistCredits     []ArtistCredit `json:"artist_credits"`
	// PreviewURL exists only while a provider response is being normalized and
	// enriched. Signed preview URLs must never enter normalized durable records.
	PreviewURL string `json:"-"`
}
type NormalizedRecordV1 struct {
	ProviderRecord     ProviderRecord      `json:"provider_record"`
	IdentityCandidates []IdentityCandidate `json:"identity_candidates"`
	Titles             []Title             `json:"titles"`
	Disambiguation     string              `json:"disambiguation,omitempty"`
	ArtistCredits      []ArtistCredit      `json:"artist_credits"`
	Classification     Classification      `json:"classification"`
	Dates              []DateValue         `json:"dates"`
	Descriptions       []Text              `json:"descriptions"`
	Annotations        []Text              `json:"annotations"`
	Genres             []WeightedTerm      `json:"genres"`
	Tags               []WeightedTerm      `json:"tags"`
	Ratings            []Rating            `json:"ratings"`
	Links              []Link              `json:"links"`
	Images             []Image             `json:"images"`
	Metrics            []Metric            `json:"metrics"`
	Editions           []Edition           `json:"editions"`
	Tracks             []Track             `json:"tracks"`
	Warnings           []string            `json:"warnings"`
	PartialFailure     bool                `json:"partial_failure"`
}
type RecordInput struct {
	ID     string
	Record NormalizedRecordV1
}

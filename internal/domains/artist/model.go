// Package artist contains provider-independent artist source records and projections.
package artist

import "time"

const (
	NormalizedSchemaVersion      = 1
	MusicBrainzNormalizerVersion = "musicbrainz-artist/v1"
	AppleNormalizerVersion       = "apple-artist/v1"
	DeezerNormalizerVersion      = "deezer-artist/v1"
	DiscogsNormalizerVersion     = "discogs-artist/v1"
	FanartNormalizerVersion      = "fanart-music-artist/v1"
	LastFMNormalizerVersion      = "lastfm-artist/v2"
	LastFMTopTracksVersion       = "lastfm-artist-top-tracks/v2"
	WikidataNormalizerVersion    = "wikidata-artist/v1"
	MergeVersion                 = "artist-merge/v1"
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

type Name struct {
	Value     string `json:"value"`
	SortValue string `json:"sort_value,omitempty"`
	Language  string `json:"language,omitempty"`
	Type      string `json:"type"`
	Primary   bool   `json:"primary,omitempty"`
	BeginDate string `json:"begin_date,omitempty"`
	EndDate   string `json:"end_date,omitempty"`
}

type Text struct {
	Value    string `json:"value"`
	Language string `json:"language,omitempty"`
	Type     string `json:"type"`
	Markup   string `json:"markup,omitempty"`
}

type Classification struct {
	ArtistType string `json:"artist_type,omitempty"`
	Gender     string `json:"gender,omitempty"`
}

type DateValue struct {
	Value     string `json:"value"`
	Precision string `json:"precision"`
	Type      string `json:"type"`
}

type Lifecycle struct {
	Dates []DateValue `json:"dates"`
	Ended *bool       `json:"ended,omitempty"`
}

type Area struct {
	ProviderID string   `json:"provider_id,omitempty"`
	Name       string   `json:"name"`
	SortName   string   `json:"sort_name,omitempty"`
	Role       string   `json:"role"`
	ISOCodes   []string `json:"iso_codes"`
}

type WeightedTerm struct {
	ProviderID string  `json:"provider_id,omitempty"`
	Name       string  `json:"name"`
	Weight     float64 `json:"weight,omitempty"`
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

type Relationship struct {
	Type            string   `json:"type"`
	Direction       string   `json:"direction,omitempty"`
	TargetProvider  string   `json:"target_provider"`
	TargetNamespace string   `json:"target_namespace"`
	TargetID        string   `json:"target_id"`
	TargetName      string   `json:"target_name,omitempty"`
	BeginDate       string   `json:"begin_date,omitempty"`
	EndDate         string   `json:"end_date,omitempty"`
	Ended           *bool    `json:"ended,omitempty"`
	Attributes      []string `json:"attributes"`
}

type SimilarArtist struct {
	ProviderID string  `json:"provider_id,omitempty"`
	Name       string  `json:"name"`
	URL        string  `json:"url,omitempty"`
	Score      float64 `json:"score,omitempty"`
}

type TopTrack struct {
	Rank            int    `json:"rank"`
	Title           string `json:"title"`
	ProviderTrackID string `json:"provider_track_id,omitempty"`
	RecordingMBID   string `json:"recording_mbid,omitempty"`
	Playcount       int64  `json:"playcount,omitempty"`
	Listeners       int64  `json:"listeners,omitempty"`
	URL             string `json:"url,omitempty"`
}

type NormalizedRecordV1 struct {
	ProviderRecord         ProviderRecord      `json:"provider_record"`
	IdentityCandidates     []IdentityCandidate `json:"identity_candidates"`
	Names                  []Name              `json:"names"`
	Disambiguation         string              `json:"disambiguation,omitempty"`
	Classification         Classification      `json:"classification"`
	Lifecycle              Lifecycle           `json:"lifecycle"`
	Areas                  []Area              `json:"areas"`
	Biographies            []Text              `json:"biographies"`
	Annotations            []Text              `json:"annotations"`
	Genres                 []WeightedTerm      `json:"genres"`
	Tags                   []WeightedTerm      `json:"tags"`
	Links                  []Link              `json:"links"`
	Images                 []Image             `json:"images"`
	Metrics                []Metric            `json:"metrics"`
	Relationships          []Relationship      `json:"relationships"`
	SimilarArtists         []SimilarArtist     `json:"similar_artists"`
	TopTracks              []TopTrack          `json:"top_tracks,omitempty"`
	TopTracksObserved      bool                `json:"top_tracks_observed,omitempty"`
	TopTracksTotal         int                 `json:"top_tracks_total,omitempty"`
	TopTracksObservationID string              `json:"top_tracks_observation_id,omitempty"`
	TopTracksObservedAt    time.Time           `json:"top_tracks_observed_at,omitempty"`
	Warnings               []string            `json:"warnings"`
	PartialFailure         bool                `json:"partial_failure"`
}

type RecordInput struct {
	ID     string
	Record NormalizedRecordV1
}

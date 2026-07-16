// Package release models issued music editions, their media, track placements,
// and the reusable recordings referenced by those placements.
package release

import "time"

const (
	NormalizedSchemaVersion    = 1
	NormalizerVersion          = "musicbrainz-release/v3"
	RecordingNormalizerVersion = "musicbrainz-recording/v3"
	MergeVersion               = "release-merge/v1"
	RecordingMergeVersion      = "recording-merge/v2"
	ProjectionSchemaVersion    = 1
)

type ProviderRecord struct {
	Provider             string    `json:"provider"`
	Namespace            string    `json:"namespace"`
	Value                string    `json:"value"`
	PrimaryObservationID string    `json:"primary_observation_id"`
	NormalizerVersion    string    `json:"normalizer_version"`
	ObservedAt           time.Time `json:"observed_at"`
	SchemaVersion        int       `json:"schema_version"`
}
type ExternalID struct {
	Provider  string `json:"provider"`
	Namespace string `json:"namespace"`
	Value     string `json:"value"`
	Evidence  string `json:"evidence,omitempty"`
}
type ArtistCredit struct {
	Position        int    `json:"position"`
	Name            string `json:"name"`
	JoinPhrase      string `json:"join_phrase,omitempty"`
	Role            string `json:"role,omitempty"`
	ArtistEntityID  string `json:"artist_entity_id,omitempty" format:"uuid"`
	ArtistProvider  string `json:"artist_provider"`
	ArtistNamespace string `json:"artist_namespace"`
	ArtistID        string `json:"artist_id"`
	ArtistName      string `json:"artist_name"`
	ResolutionState string `json:"resolution_state,omitempty" enum:"materialized,unresolved"`
}
type Label struct {
	ProviderID    string `json:"provider_id,omitempty"`
	Name          string `json:"name"`
	CatalogNumber string `json:"catalog_number,omitempty"`
}
type ReleaseEvent struct {
	Date    string `json:"date,omitempty"`
	Country string `json:"country,omitempty"`
}
type Recording struct {
	Provider       string              `json:"provider"`
	Namespace      string              `json:"namespace"`
	ProviderID     string              `json:"provider_id"`
	Title          string              `json:"title"`
	DurationMS     int64               `json:"duration_ms,omitempty"`
	ISRCs          []string            `json:"isrcs"`
	ArtistCredits  []ArtistCredit      `json:"artist_credits"`
	Credits        []PerformanceCredit `json:"credits,omitempty"`
	Disambiguation string              `json:"disambiguation,omitempty"`
	Video          bool                `json:"video,omitempty"`
	Genres         []WeightedTerm      `json:"genres,omitempty"`
	Tags           []WeightedTerm      `json:"tags,omitempty"`
	Rating         *Rating             `json:"rating,omitempty"`
	Releases       []RecordingRelease  `json:"releases,omitempty"`
	Links          []Link              `json:"links,omitempty"`
}

// PerformanceCredit is a role-bearing contribution to a recording: producer,
// engineer, mixer, vocals, instruments. Attributes carry the specifics
// MusicBrainz reports (instrument names, "lead vocals", "additional").
type PerformanceCredit struct {
	Role            string   `json:"role"`
	Attributes      []string `json:"attributes,omitempty"`
	ArtistEntityID  string   `json:"artist_entity_id,omitempty" format:"uuid"`
	ArtistProvider  string   `json:"artist_provider"`
	ArtistNamespace string   `json:"artist_namespace"`
	ArtistID        string   `json:"artist_id"`
	ArtistName      string   `json:"artist_name"`
}
type WorkRelation struct {
	ProviderID string   `json:"provider_id"`
	Title      string   `json:"title,omitempty"`
	Language   string   `json:"language,omitempty"`
	Type       string   `json:"type"`
	Attributes []string `json:"attributes,omitempty"`
}
type WeightedTerm struct {
	ProviderID string `json:"provider_id,omitempty"`
	Name       string `json:"name"`
	Count      int    `json:"count,omitempty"`
}
type Rating struct {
	Value float64 `json:"value"`
	Votes int     `json:"votes"`
}
type RecordingRelease struct {
	ReleaseEntityID             string `json:"release_entity_id,omitempty" format:"uuid"`
	ReleaseGroupEntityID        string `json:"release_group_entity_id,omitempty" format:"uuid"`
	ProviderID                  string `json:"provider_id"`
	Title                       string `json:"title"`
	Status                      string `json:"status,omitempty"`
	Date                        string `json:"date,omitempty"`
	Country                     string `json:"country,omitempty"`
	ReleaseGroupID              string `json:"release_group_id,omitempty"`
	ReleaseGroupTitle           string `json:"release_group_title,omitempty"`
	ReleaseResolutionState      string `json:"release_resolution_state,omitempty" enum:"materialized,unresolved"`
	ReleaseGroupResolutionState string `json:"release_group_resolution_state,omitempty" enum:"materialized,unresolved"`
}
type Link struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}
type NormalizedRecording struct {
	ProviderRecord ProviderRecord `json:"provider_record"`
	ExternalIDs    []ExternalID   `json:"external_ids"`
	Recording      Recording      `json:"recording"`
	WorkRelations  []WorkRelation `json:"work_relations,omitempty"`
}
type Track struct {
	ProviderID    string         `json:"provider_id"`
	Position      string         `json:"position"`
	Number        string         `json:"number"`
	Title         string         `json:"title"`
	Sequence      int            `json:"sequence"`
	DurationMS    int64          `json:"duration_ms,omitempty"`
	ArtistCredits []ArtistCredit `json:"artist_credits"`
	Recording     Recording      `json:"recording"`
	WorkRelations []WorkRelation `json:"work_relations,omitempty"`
	PreviewURL    string         `json:"-"`
}
type Medium struct {
	Position   int      `json:"position"`
	Title      string   `json:"title,omitempty"`
	Format     string   `json:"format,omitempty"`
	TrackCount int      `json:"track_count"`
	DiscIDs    []string `json:"disc_ids"`
	Tracks     []Track  `json:"tracks"`
}
type NormalizedRecord struct {
	ProviderRecord ProviderRecord `json:"provider_record"`
	ExternalIDs    []ExternalID   `json:"external_ids"`
	Title          string         `json:"title"`
	Disambiguation string         `json:"disambiguation,omitempty"`
	Status         string         `json:"status,omitempty"`
	Quality        string         `json:"quality,omitempty"`
	Packaging      string         `json:"packaging,omitempty"`
	Date           string         `json:"date,omitempty"`
	Country        string         `json:"country,omitempty"`
	Barcode        string         `json:"barcode,omitempty"`
	ASIN           string         `json:"asin,omitempty"`
	Language       string         `json:"language,omitempty"`
	Script         string         `json:"script,omitempty"`
	Link           string         `json:"link,omitempty"`
	ArtistCredits  []ArtistCredit `json:"artist_credits"`
	Labels         []Label        `json:"labels"`
	ReleaseEvents  []ReleaseEvent `json:"release_events,omitempty"`
	Genres         []WeightedTerm `json:"genres,omitempty"`
	Tags           []WeightedTerm `json:"tags,omitempty"`
	Links          []Link         `json:"links,omitempty"`
	Media          []Medium       `json:"media"`
	Warnings       []string       `json:"warnings"`
	PartialFailure bool           `json:"partial_failure"`
}

type Display struct {
	Title string `json:"title"`
	Year  int    `json:"year,omitempty"`
}
type RecordingRef struct {
	ID         string   `json:"id" format:"uuid"`
	Provider   string   `json:"provider"`
	Namespace  string   `json:"namespace"`
	ProviderID string   `json:"provider_id"`
	Title      string   `json:"title"`
	DurationMS int64    `json:"duration_ms,omitempty"`
	ISRCs      []string `json:"isrcs,omitempty"`
}
type TrackDocument struct {
	ID                string         `json:"id" format:"uuid"`
	RecordingEntityID string         `json:"recording_entity_id,omitempty" format:"uuid"`
	LyricsAvailable   bool           `json:"lyrics_available"`
	ProviderID        string         `json:"provider_id"`
	Position          string         `json:"position"`
	Number            string         `json:"number"`
	Title             string         `json:"title"`
	Sequence          int            `json:"sequence"`
	DurationMS        int64          `json:"duration_ms,omitempty"`
	ArtistCredits     []ArtistCredit `json:"artist_credits"`
	Recording         RecordingRef   `json:"recording"`
	Sources           []TrackSource  `json:"sources"`
}
type TrackSource struct {
	Provider   string `json:"provider"`
	Namespace  string `json:"namespace"`
	ProviderID string `json:"provider_id"`
	ISRC       string `json:"isrc,omitempty"`
	Link       string `json:"link,omitempty"`
}
type EditionSource struct {
	Provider   string `json:"provider"`
	Namespace  string `json:"namespace"`
	ProviderID string `json:"provider_id"`
	Title      string `json:"title"`
	Barcode    string `json:"barcode,omitempty"`
	Date       string `json:"date,omitempty"`
	Link       string `json:"link,omitempty"`
}
type MediumDocument struct {
	ID         string          `json:"id" format:"uuid"`
	Position   int             `json:"position"`
	Title      string          `json:"title,omitempty"`
	Format     string          `json:"format,omitempty"`
	TrackCount int             `json:"track_count"`
	DiscIDs    []string        `json:"disc_ids"`
	Tracks     []TrackDocument `json:"tracks"`
}
type Freshness struct {
	State      string                       `json:"state"`
	UpdatedAt  time.Time                    `json:"updated_at"`
	FreshUntil time.Time                    `json:"fresh_until"`
	Providers  map[string]ProviderFreshness `json:"providers"`
}
type ProviderFreshness struct {
	State             string    `json:"state"`
	LastSuccessAt     time.Time `json:"last_success_at"`
	LastObservationID string    `json:"last_observation_id"`
}
type SourceRef struct {
	Provider      string `json:"provider"`
	ObservationID string `json:"observation_id"`
}
type DetailDocument struct {
	SchemaVersion     int                    `json:"schema_version"`
	ProjectionVersion int64                  `json:"projection_version"`
	ID                string                 `json:"id" format:"uuid"`
	Kind              string                 `json:"kind"`
	Slug              string                 `json:"slug"`
	Display           Display                `json:"display"`
	ExternalIDs       []ExternalID           `json:"external_ids"`
	Data              ReleaseData            `json:"data"`
	Freshness         Freshness              `json:"freshness"`
	Provenance        map[string][]SourceRef `json:"provenance"`
}
type ReleaseData struct {
	Title          string           `json:"title"`
	Disambiguation string           `json:"disambiguation,omitempty"`
	Status         string           `json:"status,omitempty"`
	Quality        string           `json:"quality,omitempty"`
	Packaging      string           `json:"packaging,omitempty"`
	Date           string           `json:"date,omitempty"`
	Country        string           `json:"country,omitempty"`
	Barcode        string           `json:"barcode,omitempty"`
	ASIN           string           `json:"asin,omitempty"`
	Language       string           `json:"language,omitempty"`
	Script         string           `json:"script,omitempty"`
	ArtistCredits  []ArtistCredit   `json:"artist_credits"`
	Labels         []Label          `json:"labels"`
	ReleaseEvents  []ReleaseEvent   `json:"release_events,omitempty"`
	Genres         []WeightedTerm   `json:"genres,omitempty"`
	Tags           []WeightedTerm   `json:"tags,omitempty"`
	Links          []Link           `json:"links,omitempty"`
	Media          []MediumDocument `json:"media"`
	Sources        []EditionSource  `json:"sources"`
}
type RecordingDocument struct {
	SchemaVersion     int                    `json:"schema_version"`
	ProjectionVersion int64                  `json:"projection_version"`
	ID                string                 `json:"id" format:"uuid"`
	Kind              string                 `json:"kind"`
	Slug              string                 `json:"slug"`
	Display           Display                `json:"display"`
	ExternalIDs       []ExternalID           `json:"external_ids"`
	Data              Recording              `json:"data"`
	Freshness         Freshness              `json:"freshness"`
	Provenance        map[string][]SourceRef `json:"provenance"`
}

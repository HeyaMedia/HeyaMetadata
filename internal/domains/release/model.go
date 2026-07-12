// Package release models issued music editions, their media, track placements,
// and the reusable recordings referenced by those placements.
package release

import "time"

const (
	NormalizedSchemaVersion = 1
	NormalizerVersion       = "musicbrainz-release/v1"
	MergeVersion            = "release-merge/v1"
	ProjectionSchemaVersion = 1
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
	ArtistProvider  string `json:"artist_provider"`
	ArtistNamespace string `json:"artist_namespace"`
	ArtistID        string `json:"artist_id"`
	ArtistName      string `json:"artist_name"`
}
type Label struct {
	ProviderID    string `json:"provider_id,omitempty"`
	Name          string `json:"name"`
	CatalogNumber string `json:"catalog_number,omitempty"`
}
type Recording struct {
	Provider      string         `json:"provider"`
	Namespace     string         `json:"namespace"`
	ProviderID    string         `json:"provider_id"`
	Title         string         `json:"title"`
	DurationMS    int64          `json:"duration_ms,omitempty"`
	ISRCs         []string       `json:"isrcs"`
	ArtistCredits []ArtistCredit `json:"artist_credits"`
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
	ArtistCredits  []ArtistCredit `json:"artist_credits"`
	Labels         []Label        `json:"labels"`
	Media          []Medium       `json:"media"`
	Warnings       []string       `json:"warnings"`
	PartialFailure bool           `json:"partial_failure"`
}

type Display struct {
	Title string `json:"title"`
	Year  int    `json:"year,omitempty"`
}
type RecordingRef struct {
	ID         string   `json:"id"`
	Provider   string   `json:"provider"`
	Namespace  string   `json:"namespace"`
	ProviderID string   `json:"provider_id"`
	Title      string   `json:"title"`
	DurationMS int64    `json:"duration_ms,omitempty"`
	ISRCs      []string `json:"isrcs,omitempty"`
}
type TrackDocument struct {
	ID            string         `json:"id"`
	ProviderID    string         `json:"provider_id"`
	Position      string         `json:"position"`
	Number        string         `json:"number"`
	Title         string         `json:"title"`
	Sequence      int            `json:"sequence"`
	DurationMS    int64          `json:"duration_ms,omitempty"`
	ArtistCredits []ArtistCredit `json:"artist_credits"`
	Recording     RecordingRef   `json:"recording"`
}
type MediumDocument struct {
	ID         string          `json:"id"`
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
	ID                string                 `json:"id"`
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
	ArtistCredits  []ArtistCredit   `json:"artist_credits"`
	Labels         []Label          `json:"labels"`
	Media          []MediumDocument `json:"media"`
}
type RecordingDocument struct {
	SchemaVersion     int                    `json:"schema_version"`
	ProjectionVersion int64                  `json:"projection_version"`
	ID                string                 `json:"id"`
	Kind              string                 `json:"kind"`
	Slug              string                 `json:"slug"`
	Display           Display                `json:"display"`
	ExternalIDs       []ExternalID           `json:"external_ids"`
	Data              Recording              `json:"data"`
	Freshness         Freshness              `json:"freshness"`
	Provenance        map[string][]SourceRef `json:"provenance"`
}

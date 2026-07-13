package episodic

import "time"

type ExternalID struct {
	Provider  string `json:"provider"`
	Namespace string `json:"namespace"`
	Value     string `json:"value"`
}
type Title struct {
	Value    string `json:"value"`
	Language string `json:"language,omitempty"`
	Country  string `json:"country,omitempty"`
	Type     string `json:"type"`
}
type EpisodeNumber struct {
	Scheme string  `json:"scheme"`
	Season int     `json:"season,omitempty"`
	Number float64 `json:"number"`
}
type Episode struct {
	ID             string          `json:"id,omitempty"`
	SeasonID       string          `json:"season_id,omitempty"`
	ProviderID     string          `json:"provider_id"`
	Titles         []Title         `json:"titles"`
	Numbers        []EpisodeNumber `json:"numbers"`
	AirDate        string          `json:"air_date,omitempty"`
	RuntimeMinutes int             `json:"runtime_minutes,omitempty"`
	Summary        string          `json:"summary,omitempty"`
}
type Season struct {
	ID           string `json:"id,omitempty"`
	ProviderID   string `json:"provider_id"`
	Number       int    `json:"number"`
	Name         string `json:"name,omitempty"`
	EpisodeOrder int    `json:"episode_order,omitempty"`
	PremiereDate string `json:"premiere_date,omitempty"`
	EndDate      string `json:"end_date,omitempty"`
}
type Network struct {
	Name    string `json:"name"`
	Country string `json:"country,omitempty"`
	Type    string `json:"type,omitempty"`
}
type Image struct {
	ID            string  `json:"id,omitempty"`
	Provider      string  `json:"provider,omitempty"`
	ProviderID    string  `json:"provider_id"`
	URL           string  `json:"url,omitempty"`
	Class         string  `json:"class"`
	Language      string  `json:"language,omitempty"`
	Country       string  `json:"country,omitempty"`
	Width         int     `json:"width,omitempty"`
	Height        int     `json:"height,omitempty"`
	ProviderScore float64 `json:"provider_score,omitempty"`
}
type Rating struct {
	System   string  `json:"system"`
	Value    float64 `json:"value"`
	ScaleMin float64 `json:"scale_min"`
	ScaleMax float64 `json:"scale_max"`
	Votes    int     `json:"votes,omitempty"`
}
type Credit struct {
	PersonEntityID   string `json:"person_entity_id,omitempty"`
	Provider         string `json:"provider"`
	ProviderPersonID string `json:"provider_person_id"`
	DisplayName      string `json:"display_name"`
	CreditType       string `json:"credit_type"`
	Character        string `json:"character,omitempty"`
	Department       string `json:"department,omitempty"`
	Job              string `json:"job,omitempty"`
	Order            int    `json:"order,omitempty"`
	ProfileURL       string `json:"-"`
	ProfileImageID   string `json:"profile_image_id,omitempty"`
}
type Contributor struct {
	Provider          string `json:"provider"`
	ObservationID     string `json:"observation_id"`
	NormalizerVersion string `json:"normalizer_version"`
}
type NormalizedRecord struct {
	SchemaVersion        int           `json:"schema_version"`
	Kind                 string        `json:"kind"`
	Provider             string        `json:"provider"`
	Namespace            string        `json:"namespace"`
	ProviderID           string        `json:"provider_id"`
	PrimaryObservationID string        `json:"primary_observation_id"`
	ObservedAt           time.Time     `json:"observed_at"`
	NormalizerVersion    string        `json:"normalizer_version"`
	Contributors         []Contributor `json:"contributors,omitempty"`
	ExternalIDs          []ExternalID  `json:"external_ids"`
	Titles               []Title       `json:"titles"`
	Overview             string        `json:"overview,omitempty"`
	Format               string        `json:"format,omitempty"`
	Status               string        `json:"status,omitempty"`
	Language             string        `json:"language,omitempty"`
	Countries            []string      `json:"countries,omitempty"`
	Genres               []string      `json:"genres,omitempty"`
	StartDate            string        `json:"start_date,omitempty"`
	EndDate              string        `json:"end_date,omitempty"`
	RuntimeMinutes       int           `json:"runtime_minutes,omitempty"`
	EpisodeCount         int           `json:"episode_count,omitempty"`
	Networks             []Network     `json:"networks,omitempty"`
	Studios              []string      `json:"studios,omitempty"`
	SourceMaterial       string        `json:"source_material,omitempty"`
	Seasons              []Season      `json:"seasons,omitempty"`
	Episodes             []Episode     `json:"episodes,omitempty"`
	Images               []Image       `json:"images,omitempty"`
	Ratings              []Rating      `json:"ratings,omitempty"`
	Credits              []Credit      `json:"credits,omitempty"`
}
type Display struct {
	Title         string `json:"title"`
	OriginalTitle string `json:"original_title,omitempty"`
	Year          int    `json:"year,omitempty"`
	ImageID       string `json:"image_id,omitempty"`
}
type Classification struct {
	Format         string   `json:"format,omitempty"`
	Status         string   `json:"status,omitempty"`
	Language       string   `json:"language,omitempty"`
	Countries      []string `json:"countries,omitempty"`
	Genres         []string `json:"genres,omitempty"`
	SourceMaterial string   `json:"source_material,omitempty"`
}
type Lifecycle struct {
	StartDate string `json:"start_date,omitempty"`
	EndDate   string `json:"end_date,omitempty"`
}
type Data struct {
	Titles         []Title        `json:"titles"`
	Overview       string         `json:"overview,omitempty"`
	Classification Classification `json:"classification"`
	Lifecycle      Lifecycle      `json:"lifecycle"`
	RuntimeMinutes int            `json:"runtime_minutes,omitempty"`
	EpisodeCount   int            `json:"episode_count,omitempty"`
	Networks       []Network      `json:"networks,omitempty"`
	Studios        []string       `json:"studios,omitempty"`
	Seasons        []Season       `json:"seasons,omitempty"`
	Episodes       []Episode      `json:"episodes,omitempty"`
	Images         []Image        `json:"images,omitempty"`
	Ratings        []Rating       `json:"ratings,omitempty"`
	Credits        []Credit       `json:"credits,omitempty"`
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
type Document struct {
	SchemaVersion     int                    `json:"schema_version"`
	ProjectionVersion int64                  `json:"projection_version"`
	ID                string                 `json:"id"`
	Kind              string                 `json:"kind"`
	Slug              string                 `json:"slug"`
	Display           Display                `json:"display"`
	ExternalIDs       []ExternalID           `json:"external_ids"`
	Data              Data                   `json:"data"`
	Freshness         Freshness              `json:"freshness"`
	Provenance        map[string][]SourceRef `json:"provenance"`
}
type Summary struct {
	SchemaVersion     int       `json:"schema_version"`
	ProjectionVersion int64     `json:"projection_version"`
	ID                string    `json:"id"`
	Kind              string    `json:"kind"`
	Slug              string    `json:"slug"`
	Display           Display   `json:"display"`
	Status            string    `json:"status,omitempty"`
	Genres            []string  `json:"genres,omitempty"`
	Countries         []string  `json:"countries,omitempty"`
	Freshness         Freshness `json:"freshness"`
}

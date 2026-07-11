// Package movie contains provider-independent movie records and projections.
package movie

import "time"

const (
	NormalizedSchemaVersion = 1
	TMDBNormalizerVersion   = "tmdb-movie/v1"
	OMDBNormalizerVersion   = "omdb-movie/v1"
	MergeVersion            = "movie-merge/v1"
	ProjectionSchemaVersion = 1
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

type LocalizedText struct {
	Value    string `json:"value"`
	Language string `json:"language,omitempty"`
	Country  string `json:"country,omitempty"`
	Type     string `json:"type"`
}

type Classification struct {
	ProviderMediaType string   `json:"provider_media_type"`
	Genres            []string `json:"genres"`
	Keywords          []string `json:"keywords"`
	OriginalLanguage  string   `json:"original_language,omitempty"`
	SpokenLanguages   []string `json:"spoken_languages"`
	Countries         []string `json:"countries"`
	AnimationEvidence bool     `json:"animation_evidence"`
}

type ReleaseEvent struct {
	Country       string `json:"country"`
	Type          string `json:"type"`
	Date          string `json:"date"`
	Certification string `json:"certification,omitempty"`
	Note          string `json:"note,omitempty"`
}

type Lifecycle struct {
	RawStatus        string         `json:"raw_status,omitempty"`
	NormalizedStatus string         `json:"normalized_status,omitempty"`
	ReleaseEvents    []ReleaseEvent `json:"release_events"`
}

type Money struct {
	Amount        int64  `json:"amount"`
	Currency      string `json:"currency,omitempty"`
	CurrencyBasis string `json:"currency_basis"`
}

type Measurements struct {
	RuntimeMinutes *int     `json:"runtime_minutes,omitempty"`
	Budget         *Money   `json:"budget,omitempty"`
	Revenue        *Money   `json:"revenue,omitempty"`
	Popularity     *float64 `json:"popularity,omitempty"`
}

type Rating struct {
	System   string  `json:"system"`
	Value    float64 `json:"value"`
	ScaleMin float64 `json:"scale_min"`
	ScaleMax float64 `json:"scale_max"`
	Votes    int     `json:"votes,omitempty"`
	RawValue string  `json:"raw_value"`
}

type Link struct {
	Kind     string `json:"kind"`
	Value    string `json:"value"`
	Language string `json:"language,omitempty"`
	Country  string `json:"country,omitempty"`
}

type Video struct {
	Host        string `json:"host"`
	Key         string `json:"key"`
	Type        string `json:"type"`
	Name        string `json:"name"`
	Language    string `json:"language,omitempty"`
	Country     string `json:"country,omitempty"`
	Official    bool   `json:"official"`
	PublishedAt string `json:"published_at,omitempty"`
}

type Company struct {
	ProviderID string `json:"provider_id"`
	Name       string `json:"name"`
	Role       string `json:"role"`
	Country    string `json:"country,omitempty"`
	LogoURL    string `json:"logo_url,omitempty"`
}

type Credit struct {
	ProviderPersonID string `json:"provider_person_id"`
	DisplayName      string `json:"display_name"`
	CreditType       string `json:"credit_type"`
	Character        string `json:"character,omitempty"`
	Department       string `json:"department,omitempty"`
	Job              string `json:"job,omitempty"`
	Order            int    `json:"order,omitempty"`
	ProfileURL       string `json:"profile_url,omitempty"`
}

type Image struct {
	ProviderImageID string  `json:"provider_image_id"`
	SourceURL       string  `json:"source_url"`
	Class           string  `json:"class"`
	Width           int     `json:"width,omitempty"`
	Height          int     `json:"height,omitempty"`
	Language        string  `json:"language,omitempty"`
	Country         string  `json:"country,omitempty"`
	ProviderScore   float64 `json:"provider_score,omitempty"`
	Likes           int     `json:"likes,omitempty"`
}

type CollectionMember struct {
	ProviderID string `json:"provider_id"`
	Title      string `json:"title"`
	Year       int    `json:"year,omitempty"`
	ImageURL   string `json:"image_url,omitempty"`
	Order      int    `json:"order"`
}

type Collection struct {
	ProviderID string             `json:"provider_id"`
	Name       string             `json:"name"`
	Overview   string             `json:"overview,omitempty"`
	Images     []Image            `json:"images"`
	Members    []CollectionMember `json:"members"`
}

type Recommendation struct {
	ProviderTargetID string  `json:"provider_target_id"`
	Title            string  `json:"title"`
	Year             int     `json:"year,omitempty"`
	ImageURL         string  `json:"image_url,omitempty"`
	ProviderScore    float64 `json:"provider_score,omitempty"`
}

type NormalizedRecordV1 struct {
	ProviderRecord     ProviderRecord      `json:"provider_record"`
	IdentityCandidates []IdentityCandidate `json:"identity_candidates"`
	Titles             []LocalizedText     `json:"titles"`
	Descriptions       []LocalizedText     `json:"descriptions"`
	Taglines           []LocalizedText     `json:"taglines"`
	Classification     Classification      `json:"classification"`
	Lifecycle          Lifecycle           `json:"lifecycle"`
	Measurements       Measurements        `json:"measurements"`
	Ratings            []Rating            `json:"ratings"`
	Links              []Link              `json:"links"`
	Videos             []Video             `json:"videos"`
	Companies          []Company           `json:"companies"`
	Credits            []Credit            `json:"credits"`
	Images             []Image             `json:"images"`
	Collection         *Collection         `json:"collection,omitempty"`
	Recommendations    []Recommendation    `json:"recommendations"`
	Warnings           []string            `json:"warnings"`
	PartialFailure     bool                `json:"partial_failure"`
}

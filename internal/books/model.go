package books

import "time"

type ExternalID struct {
	Provider  string `json:"provider"`
	Namespace string `json:"namespace"`
	Value     string `json:"value"`
}
type Author struct {
	ID          string       `json:"id,omitempty"`
	Name        string       `json:"name"`
	ExternalIDs []ExternalID `json:"external_ids,omitempty"`
}
type Rating struct {
	System   string  `json:"system"`
	Value    float64 `json:"value"`
	ScaleMin float64 `json:"scale_min"`
	ScaleMax float64 `json:"scale_max"`
	Votes    int     `json:"votes,omitempty"`
}
type Image struct {
	ID       string `json:"id,omitempty"`
	Class    string `json:"class"`
	Provider string `json:"provider"`
	Width    int    `json:"width,omitempty"`
	Height   int    `json:"height,omitempty"`
}
type EditionSummary struct {
	ID            string   `json:"id"`
	Title         string   `json:"title"`
	PublishedDate string   `json:"published_date,omitempty"`
	Publishers    []string `json:"publishers,omitempty"`
	Languages     []string `json:"languages,omitempty"`
	ISBN10        []string `json:"isbn_10,omitempty"`
	ISBN13        []string `json:"isbn_13,omitempty"`
	Format        string   `json:"format,omitempty"`
	PageCount     int      `json:"page_count,omitempty"`
}
type Freshness struct {
	State      string    `json:"state"`
	UpdatedAt  time.Time `json:"updated_at"`
	FreshUntil time.Time `json:"fresh_until"`
}
type Document struct {
	SchemaVersion     int    `json:"schema_version"`
	ProjectionVersion int64  `json:"projection_version"`
	ID                string `json:"id"`
	Kind              string `json:"kind"`
	Slug              string `json:"slug"`
	Display           struct {
		Title   string `json:"title"`
		Year    int    `json:"year,omitempty"`
		ImageID string `json:"image_id,omitempty"`
	} `json:"display"`
	ExternalIDs []ExternalID `json:"external_ids"`
	Data        struct {
		Subtitle         string           `json:"subtitle,omitempty"`
		Description      string           `json:"description,omitempty"`
		Authors          []Author         `json:"authors,omitempty"`
		Subjects         []string         `json:"subjects,omitempty"`
		Languages        []string         `json:"languages,omitempty"`
		FirstPublishYear int              `json:"first_publish_year,omitempty"`
		PublishedDate    string           `json:"published_date,omitempty"`
		Publishers       []string         `json:"publishers,omitempty"`
		ISBN10           []string         `json:"isbn_10,omitempty"`
		ISBN13           []string         `json:"isbn_13,omitempty"`
		Format           string           `json:"format,omitempty"`
		PageCount        int              `json:"page_count,omitempty"`
		Ratings          []Rating         `json:"ratings,omitempty"`
		Editions         []EditionSummary `json:"editions,omitempty"`
		Images           []Image          `json:"images,omitempty"`
		WorkID           string           `json:"work_id,omitempty"`
	} `json:"data"`
	Freshness  Freshness              `json:"freshness"`
	Provenance map[string][]SourceRef `json:"provenance"`
}
type SourceRef struct {
	Provider      string `json:"provider"`
	ObservationID string `json:"observation_id"`
}
type Summary struct {
	SchemaVersion     int    `json:"schema_version"`
	ProjectionVersion int64  `json:"projection_version"`
	ID                string `json:"id"`
	Kind              string `json:"kind"`
	Slug              string `json:"slug"`
	Display           struct {
		Title   string `json:"title"`
		Year    int    `json:"year,omitempty"`
		ImageID string `json:"image_id,omitempty"`
	} `json:"display"`
	Authors   []Author  `json:"authors,omitempty"`
	Subjects  []string  `json:"subjects,omitempty"`
	Freshness Freshness `json:"freshness"`
}

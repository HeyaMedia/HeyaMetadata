package manga

import "time"

type ExternalID struct {
	Provider  string `json:"provider"`
	Namespace string `json:"namespace"`
	Value     string `json:"value"`
}
type Text struct {
	Value    string `json:"value"`
	Language string `json:"language,omitempty"`
	Type     string `json:"type,omitempty"`
	Primary  bool   `json:"primary,omitempty"`
}
type Image struct {
	ID       string `json:"id"`
	Class    string `json:"class"`
	Provider string `json:"provider"`
	Width    int    `json:"width,omitempty"`
	Height   int    `json:"height,omitempty"`
}
type Rating struct {
	System   string  `json:"system"`
	Value    float64 `json:"value"`
	ScaleMin float64 `json:"scale_min"`
	ScaleMax float64 `json:"scale_max"`
	Votes    int     `json:"votes,omitempty"`
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
		Title         string `json:"title"`
		OriginalTitle string `json:"original_title,omitempty"`
		Year          int    `json:"year,omitempty"`
		ImageID       string `json:"image_id,omitempty"`
	} `json:"display"`
	ExternalIDs []ExternalID `json:"external_ids"`
	Data        struct {
		Titles        []Text   `json:"titles"`
		Description   string   `json:"description,omitempty"`
		Subtype       string   `json:"subtype,omitempty"`
		Status        string   `json:"status,omitempty"`
		StartDate     string   `json:"start_date,omitempty"`
		EndDate       string   `json:"end_date,omitempty"`
		VolumeCount   int      `json:"volume_count,omitempty"`
		ChapterCount  int      `json:"chapter_count,omitempty"`
		Serialization string   `json:"serialization,omitempty"`
		Genres        []string `json:"genres,omitempty"`
		Ratings       []Rating `json:"ratings,omitempty"`
		Images        []Image  `json:"images,omitempty"`
	} `json:"data"`
	Freshness  Freshness              `json:"freshness"`
	Provenance map[string][]SourceRef `json:"provenance"`
}
type SourceRef struct {
	Provider      string `json:"provider"`
	ObservationID string `json:"observation_id"`
}

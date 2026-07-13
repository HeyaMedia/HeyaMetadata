// Package musicalworks owns canonical composed-work identities. A musical work
// is distinct from every score edition, performance, recording, and release.
package musicalworks

import "time"

const (
	Kind         = "musical_work"
	MergeVersion = "musical-work-combiner/v1"
)

type ExternalID struct {
	Provider  string `json:"provider"`
	Namespace string `json:"namespace"`
	Value     string `json:"value"`
}

type Composer struct {
	Name             string `json:"name"`
	Provider         string `json:"provider"`
	ProviderPersonID string `json:"provider_person_id"`
	ArtistEntityID   string `json:"artist_entity_id,omitempty"`
	Epoch            string `json:"epoch,omitempty"`
}

type Catalogue struct {
	System           string `json:"system,omitempty"`
	Number           string `json:"number,omitempty"`
	AdditionalNumber string `json:"additional_number,omitempty"`
}

type Data struct {
	Title       string    `json:"title"`
	Subtitle    string    `json:"subtitle,omitempty"`
	Genre       string    `json:"genre,omitempty"`
	Composer    Composer  `json:"composer"`
	Catalogue   Catalogue `json:"catalogue"`
	SearchTerms []string  `json:"search_terms,omitempty"`
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

type SourceRef struct {
	Provider      string `json:"provider"`
	ObservationID string `json:"observation_id"`
}

type Document struct {
	SchemaVersion     int    `json:"schema_version"`
	ProjectionVersion int64  `json:"projection_version"`
	ID                string `json:"id"`
	Kind              string `json:"kind"`
	Slug              string `json:"slug"`
	Display           struct {
		Title    string `json:"title"`
		Subtitle string `json:"subtitle,omitempty"`
	} `json:"display"`
	ExternalIDs []ExternalID           `json:"external_ids"`
	Data        Data                   `json:"data"`
	Freshness   Freshness              `json:"freshness"`
	Provenance  map[string][]SourceRef `json:"provenance"`
}

type openOpusResponse struct {
	Status struct {
		Success string `json:"success"`
	} `json:"status"`
	Composer struct {
		ID           string `json:"id"`
		Name         string `json:"name"`
		CompleteName string `json:"complete_name"`
		Epoch        string `json:"epoch"`
	} `json:"composer"`
	Work struct {
		ID               string   `json:"id"`
		Genre            string   `json:"genre"`
		Title            string   `json:"title"`
		Subtitle         string   `json:"subtitle"`
		SearchTerms      []string `json:"searchterms"`
		Catalogue        string   `json:"catalogue"`
		CatalogueNumber  string   `json:"catalogue_number"`
		AdditionalNumber string   `json:"additional_number"`
	} `json:"work"`
}

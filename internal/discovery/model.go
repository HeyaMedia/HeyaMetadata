// Package discovery ranks upstream identity candidates without turning search
// results into canonical identity claims.
package discovery

import "time"

const SchemaVersion = 1

const (
	KindMovie        = "movie"
	KindTVShow       = "tv_show"
	KindAnime        = "anime"
	KindArtist       = "artist"
	KindReleaseGroup = "release_group"
)

type Request struct {
	Kind  string `json:"kind"`
	Query string `json:"query"`
	Limit int    `json:"limit,omitempty"`
	Hints Hints  `json:"hints,omitempty"`
}
type Hints struct {
	Country   string        `json:"country,omitempty"`
	Area      string        `json:"area,omitempty"`
	Type      string        `json:"type,omitempty"`
	BeginDate string        `json:"begin_date,omitempty"`
	EndDate   string        `json:"end_date,omitempty"`
	Aliases   []string      `json:"aliases,omitempty"`
	Releases  []ReleaseHint `json:"releases,omitempty"`
}
type ReleaseHint struct {
	Title string `json:"title"`
	Year  int    `json:"year,omitempty"`
	Type  string `json:"type,omitempty"`
}
type Evidence struct {
	Field   string  `json:"field"`
	Outcome string  `json:"outcome"`
	Weight  float64 `json:"weight"`
	Detail  string  `json:"detail,omitempty"`
}
type ExternalID struct {
	Provider  string `json:"provider"`
	Namespace string `json:"namespace"`
	Value     string `json:"value"`
}
type Display struct {
	Name           string   `json:"name"`
	SortName       string   `json:"sort_name,omitempty"`
	Disambiguation string   `json:"disambiguation,omitempty"`
	Type           string   `json:"type,omitempty"`
	Country        string   `json:"country,omitempty"`
	Area           string   `json:"area,omitempty"`
	BeginDate      string   `json:"begin_date,omitempty"`
	EndDate        string   `json:"end_date,omitempty"`
	Ended          *bool    `json:"ended,omitempty"`
	Aliases        []string `json:"aliases,omitempty"`
}
type Resolution struct {
	Kind      string `json:"kind"`
	Provider  string `json:"provider"`
	Namespace string `json:"namespace"`
	Value     string `json:"value"`
}
type Candidate struct {
	Rank             int           `json:"rank"`
	Confidence       float64       `json:"confidence"`
	Match            string        `json:"match"`
	ProviderScore    int           `json:"provider_score,omitempty"`
	Identity         ExternalID    `json:"identity"`
	Display          Display       `json:"display"`
	MatchedReleases  []ReleaseHint `json:"matched_releases,omitempty"`
	Evidence         []Evidence    `json:"evidence"`
	ExistingEntityID string        `json:"existing_entity_id,omitempty"`
	Resolution       Resolution    `json:"resolution"`
}
type Result struct {
	SchemaVersion  int         `json:"schema_version"`
	Kind           string      `json:"kind"`
	Query          string      `json:"query"`
	Status         string      `json:"status"`
	Recommendation string      `json:"recommendation"`
	Candidates     []Candidate `json:"candidates"`
	Providers      []string    `json:"providers"`
	ObservedAt     time.Time   `json:"observed_at"`
	Warnings       []string    `json:"warnings,omitempty"`
}

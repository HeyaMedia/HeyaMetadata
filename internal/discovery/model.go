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
	Country       string        `json:"country,omitempty"`
	Language      string        `json:"language,omitempty"`
	Area          string        `json:"area,omitempty"`
	Type          string        `json:"type,omitempty"`
	Year          int           `json:"year,omitempty"`
	Date          string        `json:"date,omitempty"`
	OriginalTitle string        `json:"original_title,omitempty"`
	BeginDate     string        `json:"begin_date,omitempty"`
	EndDate       string        `json:"end_date,omitempty"`
	Aliases       []string      `json:"aliases,omitempty"`
	Artists       []string      `json:"artists,omitempty"`
	ArtistIDs     []string      `json:"artist_ids,omitempty"`
	Tracks        []string      `json:"tracks,omitempty"`
	Network       string        `json:"network,omitempty"`
	Status        string        `json:"status,omitempty"`
	Season        string        `json:"season,omitempty"`
	Source        string        `json:"source,omitempty"`
	EpisodeCount  int           `json:"episode_count,omitempty"`
	Studios       []string      `json:"studios,omitempty"`
	Episodes      []EpisodeHint `json:"episodes,omitempty"`
	Releases      []ReleaseHint `json:"releases,omitempty"`
}
type EpisodeHint struct {
	Title  string `json:"title,omitempty"`
	Season int    `json:"season,omitempty"`
	Number int    `json:"number,omitempty"`
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
	Name           string          `json:"name,omitempty"`
	Title          string          `json:"title,omitempty"`
	OriginalTitle  string          `json:"original_title,omitempty"`
	SortName       string          `json:"sort_name,omitempty"`
	Disambiguation string          `json:"disambiguation,omitempty"`
	Type           string          `json:"type,omitempty"`
	Country        string          `json:"country,omitempty"`
	Countries      []string        `json:"countries,omitempty"`
	Language       string          `json:"language,omitempty"`
	Year           int             `json:"year,omitempty"`
	Date           string          `json:"date,omitempty"`
	Popularity     float64         `json:"popularity,omitempty"`
	Artists        []ArtistDisplay `json:"artists,omitempty"`
	SecondaryTypes []string        `json:"secondary_types,omitempty"`
	Network        string          `json:"network,omitempty"`
	Status         string          `json:"status,omitempty"`
	Season         string          `json:"season,omitempty"`
	Source         string          `json:"source,omitempty"`
	EpisodeCount   int             `json:"episode_count,omitempty"`
	Studios        []string        `json:"studios,omitempty"`
	Area           string          `json:"area,omitempty"`
	BeginDate      string          `json:"begin_date,omitempty"`
	EndDate        string          `json:"end_date,omitempty"`
	Ended          *bool           `json:"ended,omitempty"`
	Aliases        []string        `json:"aliases,omitempty"`
}
type ArtistDisplay struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name"`
	Join string `json:"join,omitempty"`
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
	MatchedTracks    []string      `json:"matched_tracks,omitempty"`
	MatchedEpisodes  []EpisodeHint `json:"matched_episodes,omitempty"`
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

// Package discovery ranks upstream identity candidates without turning search
// results into canonical identity claims.
package discovery

import "time"

const SchemaVersion = 2

const (
	KindMovie        = "movie"
	KindTVShow       = "tv_show"
	KindAnime        = "anime"
	KindArtist       = "artist"
	KindReleaseGroup = "release_group"
	KindRecording    = "recording"
	KindMusicalWork  = "musical_work"
	KindBookWork     = "book_work"
	KindManga        = "manga"
	KindMangaVolume  = "manga_volume"
	KindComicVolume  = "comic_volume"
)

type Request struct {
	Kind        string       `json:"kind"`
	Query       string       `json:"query,omitempty"`
	Identifiers []Identifier `json:"identifiers,omitempty" maxItems:"50"`
	Limit       int          `json:"limit,omitempty"`
	Hints       Hints        `json:"hints,omitempty"`
}
type Identifier struct {
	Scheme string `json:"scheme" minLength:"1" maxLength:"50"`
	Value  string `json:"value" minLength:"1" maxLength:"500"`
}
type IdentifierEvidence struct {
	Scheme  string `json:"scheme"`
	Value   string `json:"value"`
	Outcome string `json:"outcome" enum:"resolved,corroborating,unused,unsupported,conflict"`
	Detail  string `json:"detail,omitempty"`
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
	ArtistIDs     []string      `json:"-"`
	Composers     []string      `json:"composers,omitempty"`
	ComposerIDs   []string      `json:"-"`
	Catalogue     string        `json:"catalogue,omitempty"`
	Tracks        []string      `json:"tracks,omitempty"`
	Network       string        `json:"network,omitempty"`
	Status        string        `json:"status,omitempty"`
	Season        string        `json:"season,omitempty"`
	Source        string        `json:"source,omitempty"`
	EpisodeCount  int           `json:"episode_count,omitempty"`
	Studios       []string      `json:"studios,omitempty"`
	Episodes      []EpisodeHint `json:"episodes,omitempty"`
	Releases      []ReleaseHint `json:"releases,omitempty"`
	DurationMS    int64         `json:"duration_ms,omitempty"`
	ISRCs         []string      `json:"isrcs,omitempty"`
	Authors       []string      `json:"authors,omitempty"`
	ISBNs         []string      `json:"isbns,omitempty"`
}
type EpisodeHint struct {
	Title  string `json:"title,omitempty"`
	Season int    `json:"season,omitempty"`
	Number int    `json:"number,omitempty"`
}
type ReleaseHint struct {
	Title       string       `json:"title"`
	Year        int          `json:"year,omitempty"`
	Type        string       `json:"type,omitempty"`
	Identifiers []Identifier `json:"identifiers,omitempty" maxItems:"20"`
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
	Languages      []string        `json:"languages,omitempty"`
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
	DurationMS     int64           `json:"duration_ms,omitempty"`
	ISRCs          []string        `json:"isrcs,omitempty"`
	Releases       []ReleaseHint   `json:"releases,omitempty"`
	Authors        []string        `json:"authors,omitempty"`
	EditionCount   int             `json:"edition_count,omitempty"`
	ISBNs          []string        `json:"isbns,omitempty"`
	Catalogue      string          `json:"catalogue,omitempty"`
}
type ArtistDisplay struct {
	ID   string `json:"-"`
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
	CandidateRef     string        `json:"candidate_ref" format:"uuid"`
	Confidence       float64       `json:"confidence"`
	Match            string        `json:"match"`
	ProviderScore    int           `json:"-"`
	Identity         ExternalID    `json:"-"`
	Display          Display       `json:"display"`
	MatchedReleases  []ReleaseHint `json:"matched_releases,omitempty"`
	MatchedTracks    []string      `json:"matched_tracks,omitempty"`
	MatchedEpisodes  []EpisodeHint `json:"matched_episodes,omitempty"`
	Evidence         []Evidence    `json:"evidence"`
	ExistingEntityID string        `json:"-"`
	Resolution       Resolution    `json:"-"`
	// artistReleaseMatches is private provider evidence retained only long
	// enough to reconcile artist discovery candidates. Public candidates keep
	// the caller's matched hints, never the provider routing identifiers.
	artistReleaseMatches []artistReleaseMatch
}

// ArtistIdentityConvergence is an internal hand-off from discovery to the
// durable worker. The worker materializes the newly proven MusicBrainz root on
// the already existing canonical artist before returning that entity ID.
type ArtistIdentityConvergence struct {
	EntityID                   string
	MusicBrainzID              string
	MusicBrainzReleaseGroupID  string
	StorefrontProvider         string
	StorefrontArtistID         string
	StorefrontReleaseNamespace string
	StorefrontReleaseID        string
}
type Result struct {
	SchemaVersion      int                        `json:"schema_version"`
	Kind               string                     `json:"kind"`
	Query              string                     `json:"query,omitempty"`
	Status             string                     `json:"status" enum:"completed,needs_selection"`
	Recommendation     string                     `json:"recommendation"`
	EntityID           string                     `json:"entity_id,omitempty" format:"uuid"`
	Candidates         []Candidate                `json:"candidates,omitempty"`
	IdentifierEvidence []IdentifierEvidence       `json:"identifier_evidence,omitempty"`
	Providers          []string                   `json:"-"`
	ObservedAt         time.Time                  `json:"observed_at"`
	Warnings           []string                   `json:"warnings,omitempty"`
	ArtistConvergence  *ArtistIdentityConvergence `json:"-"`
}

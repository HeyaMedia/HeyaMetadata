package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/providercache"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/tmdb"
	"github.com/jackc/pgx/v5"
)

type tmdbMovieSearch struct {
	Results []struct {
		ID               int64    `json:"id"`
		Title            string   `json:"title"`
		OriginalTitle    string   `json:"original_title"`
		OriginalLanguage string   `json:"original_language"`
		ReleaseDate      string   `json:"release_date"`
		OriginCountry    []string `json:"origin_country"`
		Popularity       float64  `json:"popularity"`
	} `json:"results"`
}

func (s *Service) DiscoverMovie(ctx context.Context, request Request, jobID int64, apiKey string) (Result, error) {
	request = NormalizeRequest(request)
	if request.Kind != KindMovie {
		return Result{}, fmt.Errorf("movie discovery requires kind movie")
	}
	if request.Query == "" {
		return Result{}, fmt.Errorf("discovery query is required")
	}
	base := tmdb.New(s.runtime.Config.Providers.TMDB)
	resolver, err := providercache.New(s.runtime, "tmdb-discovery/v1", base.Capability().RawRetention, base.Capability().ResponseCache, jobID)
	if err != nil {
		return Result{}, err
	}
	client := tmdb.NewCached(s.runtime.Config.Providers.TMDB, resolver, apiKey)
	// Keep the year as ranking evidence rather than an upstream hard filter: a
	// regional premiere or slightly wrong client hint must not hide the match.
	payload, err := client.SearchMovies(ctx, request.Query, 0, 1)
	if err != nil {
		return Result{}, err
	}
	if payload.StatusCode != http.StatusOK {
		return Result{}, &providers.StatusError{Provider: "tmdb", StatusCode: payload.StatusCode}
	}
	var source tmdbMovieSearch
	if err := json.Unmarshal(payload.Body, &source); err != nil {
		return Result{}, fmt.Errorf("decode TMDB movie search: %w", err)
	}
	candidates := make([]Candidate, 0, len(source.Results))
	for index, value := range source.Results {
		if value.ID < 1 || strings.TrimSpace(value.Title) == "" {
			continue
		}
		id := strconv.FormatInt(value.ID, 10)
		providerScore := max(1, 100-index*4)
		candidate := Candidate{
			ProviderScore: providerScore,
			Identity:      ExternalID{Provider: "tmdb", Namespace: "movie", Value: id},
			Display:       Display{Title: value.Title, OriginalTitle: value.OriginalTitle, Language: strings.ToLower(value.OriginalLanguage), Countries: cleanSortedUpper(value.OriginCountry), Year: releaseYear(value.ReleaseDate), Date: value.ReleaseDate, Popularity: value.Popularity},
			Resolution:    Resolution{Kind: KindMovie, Provider: "tmdb", Namespace: "movie", Value: id},
		}
		scoreMovieCandidate(request, &candidate)
		var entityID string
		err := s.runtime.DB.QueryRow(ctx, `SELECT entity_id FROM external_id_claims WHERE entity_kind='movie' AND provider='tmdb' AND namespace='movie' AND normalized_value=$1 AND state='accepted'`, id).Scan(&entityID)
		if err == nil {
			candidate.ExistingEntityID = entityID
		} else if err != pgx.ErrNoRows {
			return Result{}, err
		}
		candidates = append(candidates, candidate)
	}
	sortCandidates(candidates)
	if len(candidates) > request.Limit {
		candidates = candidates[:request.Limit]
	}
	for i := range candidates {
		candidates[i].Rank = i + 1
	}
	return Result{SchemaVersion: SchemaVersion, Kind: KindMovie, Query: request.Query, Status: "completed", Recommendation: recommendation(candidates), Candidates: candidates, Providers: []string{"tmdb"}, ObservedAt: time.Now().UTC()}, nil
}

func scoreMovieCandidate(request Request, candidate *Candidate) {
	score := float64(candidate.ProviderScore) / 100 * .12
	candidate.Evidence = append(candidate.Evidence, Evidence{Field: "provider_rank", Outcome: "support", Weight: round(float64(candidate.ProviderScore) / 100 * .12), Detail: fmt.Sprintf("TMDB search relevance %d/100", candidate.ProviderScore)})
	query := normalizedText(request.Query)
	title := normalizedText(candidate.Display.Title)
	original := normalizedText(candidate.Display.OriginalTitle)
	weight, outcome := similarity(query, title)*.42, "fuzzy"
	if query == title {
		weight, outcome = .42, "exact"
	} else if query != "" && query == original {
		weight, outcome = .40, "exact_original_title"
	}
	score += weight
	candidate.Evidence = append(candidate.Evidence, Evidence{Field: "title", Outcome: outcome, Weight: round(weight), Detail: candidate.Display.Title})
	if request.Hints.Year > 0 {
		weight, outcome = -.08, "mismatch"
		delta := abs(request.Hints.Year - candidate.Display.Year)
		if delta == 0 {
			weight, outcome = .18, "exact"
		} else if delta == 1 {
			weight, outcome = .08, "near"
		}
		score += weight
		candidate.Evidence = append(candidate.Evidence, Evidence{Field: "year", Outcome: outcome, Weight: weight, Detail: strconv.Itoa(candidate.Display.Year)})
	}
	if request.Hints.Date != "" {
		weight, outcome = -.05, "mismatch"
		if request.Hints.Date == candidate.Display.Date {
			weight, outcome = .18, "exact"
		} else if releaseYear(request.Hints.Date) > 0 && releaseYear(request.Hints.Date) == candidate.Display.Year {
			weight, outcome = .08, "year_match"
		}
		score += weight
		candidate.Evidence = append(candidate.Evidence, Evidence{Field: "date", Outcome: outcome, Weight: weight, Detail: candidate.Display.Date})
	}
	if request.Hints.OriginalTitle != "" {
		weight, outcome = -.04, "mismatch"
		if normalizedText(request.Hints.OriginalTitle) == original {
			weight, outcome = .12, "exact"
		}
		score += weight
		candidate.Evidence = append(candidate.Evidence, Evidence{Field: "original_title", Outcome: outcome, Weight: weight, Detail: candidate.Display.OriginalTitle})
	}
	if request.Hints.Language != "" {
		weight, outcome = -.03, "mismatch"
		if request.Hints.Language == candidate.Display.Language {
			weight, outcome = .08, "exact"
		}
		score += weight
		candidate.Evidence = append(candidate.Evidence, Evidence{Field: "language", Outcome: outcome, Weight: weight, Detail: candidate.Display.Language})
	}
	if request.Hints.Country != "" {
		weight, outcome = -.03, "mismatch"
		for _, country := range candidate.Display.Countries {
			if country == request.Hints.Country {
				weight, outcome = .08, "exact"
				break
			}
		}
		score += weight
		candidate.Evidence = append(candidate.Evidence, Evidence{Field: "country", Outcome: outcome, Weight: weight, Detail: strings.Join(candidate.Display.Countries, ",")})
	}
	if len(request.Hints.Aliases) > 0 {
		matched := 0
		for _, alias := range request.Hints.Aliases {
			if normalizedText(alias) == title || normalizedText(alias) == original {
				matched++
			}
		}
		weight = .1 * float64(matched) / float64(len(request.Hints.Aliases))
		score += weight
		candidate.Evidence = append(candidate.Evidence, Evidence{Field: "alternate_titles", Outcome: fmt.Sprintf("%d_of_%d", matched, len(request.Hints.Aliases)), Weight: round(weight)})
	}
	candidate.Confidence = round(math.Max(0, math.Min(.99, score)))
	setMatch(candidate)
}

func cleanSortedUpper(values []string) []string {
	for i := range values {
		values[i] = strings.ToUpper(strings.TrimSpace(values[i]))
	}
	return cleanSorted(values)
}

func sortCandidates(candidates []Candidate) {
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Confidence != candidates[j].Confidence {
			return candidates[i].Confidence > candidates[j].Confidence
		}
		if candidates[i].ProviderScore != candidates[j].ProviderScore {
			return candidates[i].ProviderScore > candidates[j].ProviderScore
		}
		return candidates[i].Identity.Value < candidates[j].Identity.Value
	})
}

func setMatch(candidate *Candidate) {
	switch {
	case candidate.Confidence >= .85:
		candidate.Match = "strong"
	case candidate.Confidence >= .65:
		candidate.Match = "likely"
	case candidate.Confidence >= .45:
		candidate.Match = "possible"
	default:
		candidate.Match = "weak"
	}
}

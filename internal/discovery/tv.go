package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/providercache"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/tmdb"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/tvmaze"
	"github.com/jackc/pgx/v5"
)

type tvMazeSearchHit struct {
	Score float64    `json:"score"`
	Show  tvMazeShow `json:"show"`
}

type tmdbTVSearch struct {
	Results []struct {
		ID               int64    `json:"id"`
		Name             string   `json:"name"`
		OriginalName     string   `json:"original_name"`
		OriginalLanguage string   `json:"original_language"`
		FirstAirDate     string   `json:"first_air_date"`
		OriginCountry    []string `json:"origin_country"`
		GenreIDs         []int    `json:"genre_ids"`
		Popularity       float64  `json:"popularity"`
	} `json:"results"`
}

type tvMazeShow struct {
	ID        int64    `json:"id"`
	Name      string   `json:"name"`
	Type      string   `json:"type"`
	Language  string   `json:"language"`
	Status    string   `json:"status"`
	Premiered string   `json:"premiered"`
	Ended     string   `json:"ended"`
	Genres    []string `json:"genres"`
	Network   *struct {
		Name    string `json:"name"`
		Country *struct {
			Code string `json:"code"`
		} `json:"country"`
	} `json:"network"`
	WebChannel *struct {
		Name    string `json:"name"`
		Country *struct {
			Code string `json:"code"`
		} `json:"country"`
	} `json:"webChannel"`
	Externals struct {
		TVDB   int64  `json:"thetvdb"`
		IMDb   string `json:"imdb"`
		TVRage int64  `json:"tvrage"`
	} `json:"externals"`
	Embedded struct {
		AKAs []struct {
			Name    string `json:"name"`
			Country *struct {
				Code string `json:"code"`
			} `json:"country"`
		} `json:"akas"`
		Episodes []struct {
			Name   string `json:"name"`
			Season int    `json:"season"`
			Number int    `json:"number"`
		} `json:"episodes"`
	} `json:"_embedded"`
}

func (s *Service) DiscoverTV(ctx context.Context, request Request, jobID int64, apiKey string) (Result, error) {
	request = NormalizeRequest(request)
	if request.Kind != KindTVShow || request.Query == "" {
		return Result{}, fmt.Errorf("TV discovery requires kind tv_show and a query")
	}
	candidates, err := s.discoverTMDBEpisodicCandidates(ctx, request, jobID, apiKey, false)
	if err != nil {
		return Result{}, err
	}
	if hasPlausibleCandidate(candidates) {
		return episodicDiscoveryResult(request, candidates, []string{"tmdb"}), nil
	}
	return s.discoverTVMaze(ctx, request, jobID)
}

func (s *Service) discoverTMDBEpisodicCandidates(ctx context.Context, request Request, jobID int64, apiKey string, animationOnly bool) ([]Candidate, error) {
	base := tmdb.New(s.runtime.Config.Providers.TMDB)
	resolver, err := providercache.New(s.runtime, "tmdb-tv-discovery/v1", base.Capability().RawRetention, base.Capability().ResponseCache, jobID)
	if err != nil {
		return nil, err
	}
	payload, err := tmdb.NewCached(s.runtime.Config.Providers.TMDB, resolver, apiKey).SearchTV(ctx, request.Query, 0, 1)
	if err != nil {
		return nil, err
	}
	if payload.StatusCode != http.StatusOK {
		return nil, &providers.StatusError{Provider: "tmdb", StatusCode: payload.StatusCode}
	}
	var source tmdbTVSearch
	if err := json.Unmarshal(payload.Body, &source); err != nil {
		return nil, fmt.Errorf("decode TMDB TV search: %w", err)
	}
	candidates := make([]Candidate, 0, len(source.Results))
	for index, value := range source.Results {
		if value.ID < 1 || strings.TrimSpace(value.Name) == "" || (animationOnly && !containsInt(value.GenreIDs, 16)) {
			continue
		}
		id := strconv.FormatInt(value.ID, 10)
		aliases := []string{}
		if normalizedText(value.OriginalName) != normalizedText(value.Name) {
			aliases = append(aliases, value.OriginalName)
		}
		country := ""
		if len(value.OriginCountry) > 0 {
			country = strings.ToUpper(value.OriginCountry[0])
		}
		kind := request.Kind
		candidate := Candidate{
			ProviderScore: max(1, 100-index*4),
			Identity:      ExternalID{Provider: "tmdb", Namespace: "tv", Value: id},
			Display: Display{
				Name: value.Name, OriginalTitle: value.OriginalName, Type: map[bool]string{true: "anime", false: "series"}[animationOnly],
				Language: strings.ToLower(value.OriginalLanguage), Year: releaseYear(value.FirstAirDate), Date: value.FirstAirDate,
				Country: country, Countries: cleanSortedUpper(value.OriginCountry), Popularity: value.Popularity, Aliases: cleanSorted(aliases),
			},
			Resolution: Resolution{Kind: kind, Provider: "tmdb", Namespace: "tv", Value: id},
		}
		if kind == KindAnime {
			scoreAnimeCandidate(request, &candidate)
		} else {
			scoreTVCandidate(request, &candidate)
		}
		var entityID string
		e := s.runtime.DB.QueryRow(ctx, `SELECT entity_id FROM external_id_claims WHERE entity_kind=$1 AND provider='tmdb' AND namespace='tv' AND normalized_value=$2 AND state='accepted'`, kind, id).Scan(&entityID)
		if e == nil {
			candidate.ExistingEntityID = entityID
		} else if e != pgx.ErrNoRows {
			return nil, e
		}
		candidates = append(candidates, candidate)
	}
	sortCandidates(candidates)
	return candidates, nil
}

func (s *Service) discoverTVMaze(ctx context.Context, request Request, jobID int64) (Result, error) {
	base := tvmaze.New(s.runtime.Config.Providers.TVMaze)
	resolver, err := providercache.New(s.runtime, "tvmaze-tv-discovery/v1", base.Capability().RawRetention, base.Capability().ResponseCache, jobID)
	if err != nil {
		return Result{}, err
	}
	client := tvmaze.NewCached(s.runtime.Config.Providers.TVMaze, resolver)
	payload, err := client.Search(ctx, "show", request.Query)
	if err != nil {
		return Result{}, err
	}
	if payload.StatusCode != http.StatusOK {
		return Result{}, &providers.StatusError{Provider: "tvmaze", StatusCode: payload.StatusCode}
	}
	var hits []tvMazeSearchHit
	if err := json.Unmarshal(payload.Body, &hits); err != nil {
		return Result{}, fmt.Errorf("decode TVMaze show search: %w", err)
	}
	if len(request.Hints.Episodes) > 0 {
		for i := 0; i < min(len(hits), 5); i++ {
			payloads, collectErr := client.Collect(ctx, providers.Identifier{Provider: "tvmaze", Namespace: "show", Value: strconv.FormatInt(hits[i].Show.ID, 10)})
			if collectErr != nil {
				return Result{}, collectErr
			}
			if len(payloads) > 0 && payloads[len(payloads)-1].StatusCode == http.StatusOK {
				_ = json.Unmarshal(payloads[len(payloads)-1].Body, &hits[i].Show)
			}
		}
	}
	candidates := make([]Candidate, 0, len(hits))
	for _, hit := range hits {
		if hit.Show.ID < 1 || strings.TrimSpace(hit.Show.Name) == "" {
			continue
		}
		id := strconv.FormatInt(hit.Show.ID, 10)
		aliases := make([]string, 0, len(hit.Show.Embedded.AKAs))
		for _, alias := range hit.Show.Embedded.AKAs {
			aliases = append(aliases, alias.Name)
		}
		network, country := tvNetwork(hit.Show)
		matched := matchTVEpisodes(request.Hints.Episodes, hit.Show.Embedded.Episodes)
		providerScore := int(math.Round(math.Min(1, hit.Score) * 100))
		candidate := Candidate{ProviderScore: providerScore, Identity: ExternalID{Provider: "tvmaze", Namespace: "show", Value: id}, Display: Display{Name: hit.Show.Name, Type: normalizeType(hit.Show.Type), Language: tvLanguageCode(hit.Show.Language), Year: releaseYear(hit.Show.Premiered), Date: hit.Show.Premiered, EndDate: hit.Show.Ended, Country: country, Network: network, Status: normalizeType(hit.Show.Status), Aliases: cleanSorted(aliases)}, MatchedEpisodes: matched, Resolution: Resolution{Kind: KindTVShow, Provider: "tvmaze", Namespace: "show", Value: id}}
		scoreTVCandidate(request, &candidate)
		var entityID string
		err := s.runtime.DB.QueryRow(ctx, `SELECT entity_id FROM external_id_claims WHERE entity_kind='tv_show' AND provider='tvmaze' AND namespace='show' AND normalized_value=$1 AND state='accepted'`, id).Scan(&entityID)
		if err == nil {
			candidate.ExistingEntityID = entityID
		} else if err != pgx.ErrNoRows {
			return Result{}, err
		}
		candidates = append(candidates, candidate)
	}
	return episodicDiscoveryResult(request, candidates, []string{"tmdb", "tvmaze"}), nil
}

func episodicDiscoveryResult(request Request, candidates []Candidate, providersUsed []string) Result {
	sortCandidates(candidates)
	if len(candidates) > request.Limit {
		candidates = candidates[:request.Limit]
	}
	for i := range candidates {
		candidates[i].Rank = i + 1
	}
	return Result{SchemaVersion: SchemaVersion, Kind: request.Kind, Query: request.Query, Status: "completed", Recommendation: recommendation(candidates), Candidates: candidates, Providers: providersUsed, ObservedAt: time.Now().UTC()}
}

func hasPlausibleCandidate(candidates []Candidate) bool {
	return len(candidates) > 0 && candidates[0].Confidence >= .45
}

func containsInt(values []int, want int) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func tvNetwork(show tvMazeShow) (string, string) {
	if show.Network != nil {
		country := ""
		if show.Network.Country != nil {
			country = strings.ToUpper(show.Network.Country.Code)
		}
		return show.Network.Name, country
	}
	if show.WebChannel != nil {
		country := ""
		if show.WebChannel.Country != nil {
			country = strings.ToUpper(show.WebChannel.Country.Code)
		}
		return show.WebChannel.Name, country
	}
	return "", ""
}

func matchTVEpisodes(hints []EpisodeHint, episodes []struct {
	Name   string `json:"name"`
	Season int    `json:"season"`
	Number int    `json:"number"`
}) []EpisodeHint {
	out := []EpisodeHint{}
	for _, hint := range hints {
		for _, episode := range episodes {
			titleOK := hint.Title == "" || normalizedText(hint.Title) == normalizedText(episode.Name)
			seasonOK := hint.Season == 0 || hint.Season == episode.Season
			numberOK := hint.Number == 0 || hint.Number == episode.Number
			if titleOK && seasonOK && numberOK {
				out = append(out, hint)
				break
			}
		}
	}
	return out
}

func scoreTVCandidate(request Request, c *Candidate) {
	score := float64(c.ProviderScore) / 100 * .18
	c.Evidence = append(c.Evidence, Evidence{Field: "provider_score", Outcome: "support", Weight: round(score), Detail: fmt.Sprintf("%s search score %d/100", strings.ToUpper(c.Identity.Provider), c.ProviderScore)})
	w, o := episodicTitleMatch(request, c.Display.Name, c.Display.Aliases, .4, .37)
	score += w
	c.Evidence = append(c.Evidence, Evidence{Field: "title", Outcome: o, Weight: round(w), Detail: c.Display.Name})
	if request.Hints.Year > 0 {
		w, o = -.06, "mismatch"
		d := abs(request.Hints.Year - c.Display.Year)
		if d == 0 {
			w, o = .14, "exact"
		} else if d == 1 {
			w, o = .06, "near"
		}
		score += w
		c.Evidence = append(c.Evidence, Evidence{Field: "year", Outcome: o, Weight: w, Detail: strconv.Itoa(c.Display.Year)})
	}
	if request.Hints.Country != "" {
		w, o = -.03, "mismatch"
		if request.Hints.Country == c.Display.Country {
			w, o = .08, "exact"
		}
		score += w
		c.Evidence = append(c.Evidence, Evidence{Field: "country", Outcome: o, Weight: w, Detail: c.Display.Country})
	}
	if request.Hints.Language != "" {
		w, o = -.03, "mismatch"
		if request.Hints.Language == c.Display.Language {
			w, o = .07, "exact"
		}
		score += w
		c.Evidence = append(c.Evidence, Evidence{Field: "language", Outcome: o, Weight: w, Detail: c.Display.Language})
	}
	if request.Hints.Network != "" {
		w, o = -.03, "mismatch"
		if normalizedText(request.Hints.Network) == normalizedText(c.Display.Network) {
			w, o = .09, "exact"
		}
		score += w
		c.Evidence = append(c.Evidence, Evidence{Field: "network", Outcome: o, Weight: w, Detail: c.Display.Network})
	}
	if request.Hints.Status != "" {
		w, o = -.02, "mismatch"
		if request.Hints.Status == c.Display.Status {
			w, o = .06, "exact"
		}
		score += w
		c.Evidence = append(c.Evidence, Evidence{Field: "status", Outcome: o, Weight: w, Detail: c.Display.Status})
	}
	if len(request.Hints.Episodes) > 0 {
		w = .18 * float64(len(c.MatchedEpisodes)) / float64(len(request.Hints.Episodes))
		score += w
		c.Evidence = append(c.Evidence, Evidence{Field: "episodes", Outcome: fmt.Sprintf("%d_of_%d", len(c.MatchedEpisodes), len(request.Hints.Episodes)), Weight: round(w)})
	}
	c.Confidence = round(math.Max(0, math.Min(.99, score)))
	setMatch(c)
}

func episodicTitleMatch(request Request, candidateName string, candidateAliases []string, exactWeight, aliasWeight float64) (float64, string) {
	name := normalizedText(candidateName)
	bestWeight, bestOutcome := 0.0, "fuzzy"
	queries := append([]string{request.Query}, request.Hints.Aliases...)
	for index, value := range queries {
		query := normalizedText(value)
		if query == "" {
			continue
		}
		weight, outcome := similarity(query, name)*exactWeight, "fuzzy"
		if query == name {
			weight, outcome = exactWeight, "exact"
			if index > 0 {
				outcome = "exact_hint_alias"
			}
		} else {
			for _, alias := range candidateAliases {
				if query == normalizedText(alias) {
					weight, outcome = aliasWeight, "exact_alias"
					if index > 0 {
						outcome = "exact_hint_alias"
					}
					break
				}
			}
		}
		if weight > bestWeight {
			bestWeight, bestOutcome = weight, outcome
		}
	}
	return bestWeight, bestOutcome
}

func tvLanguageCode(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if code := map[string]string{"english": "en", "japanese": "ja", "korean": "ko", "chinese": "zh", "french": "fr", "german": "de", "spanish": "es", "danish": "da", "swedish": "sv", "norwegian": "no"}[value]; code != "" {
		return code
	}
	return value
}

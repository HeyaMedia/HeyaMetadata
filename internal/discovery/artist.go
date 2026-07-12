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
	"unicode"

	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/HeyaMedia/HeyaMetadata/internal/providercache"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/musicbrainz"
	"github.com/jackc/pgx/v5"
)

type Service struct{ runtime *platform.Runtime }

func NewService(runtime *platform.Runtime) *Service { return &Service{runtime: runtime} }

type mbArtistSearch struct {
	Artists []struct {
		ID             string `json:"id"`
		Name           string `json:"name"`
		SortName       string `json:"sort-name"`
		Disambiguation string `json:"disambiguation"`
		Type           string `json:"type"`
		Country        string `json:"country"`
		Score          int    `json:"score"`
		Area           *struct {
			Name string `json:"name"`
		} `json:"area"`
		LifeSpan struct {
			Begin string `json:"begin"`
			End   string `json:"end"`
			Ended *bool  `json:"ended"`
		} `json:"life-span"`
		Aliases []struct {
			Name string `json:"name"`
		} `json:"aliases"`
	} `json:"artists"`
}
type mbReleaseSearch struct {
	ReleaseGroups []struct {
		Title            string `json:"title"`
		FirstReleaseDate string `json:"first-release-date"`
		PrimaryType      string `json:"primary-type"`
		ArtistCredit     []struct {
			Artist struct {
				ID string `json:"id"`
			} `json:"artist"`
		} `json:"artist-credit"`
	} `json:"release-groups"`
}

func (s *Service) DiscoverArtist(ctx context.Context, request Request, jobID int64) (Result, error) {
	request = NormalizeRequest(request)
	if request.Kind != KindArtist {
		return Result{}, fmt.Errorf("artist discovery requires kind artist")
	}
	if request.Query == "" {
		return Result{}, fmt.Errorf("discovery query is required")
	}
	base := musicbrainz.New(s.runtime.Config.Providers.MusicBrainz)
	resolver, err := providercache.New(s.runtime, "musicbrainz-discovery/v1", base.Capability().RawRetention, base.Capability().ResponseCache, jobID)
	if err != nil {
		return Result{}, err
	}
	client := musicbrainz.NewCached(s.runtime.Config.Providers.MusicBrainz, resolver)
	query := `(artist:"` + escapeLucene(request.Query) + `" OR alias:"` + escapeLucene(request.Query) + `")`
	payload, err := client.Search(ctx, "artist", query, min(100, max(25, request.Limit*4)), 0)
	if err != nil {
		return Result{}, err
	}
	if payload.StatusCode != http.StatusOK {
		return Result{}, &providers.StatusError{Provider: "musicbrainz", StatusCode: payload.StatusCode}
	}
	var source mbArtistSearch
	if err := json.Unmarshal(payload.Body, &source); err != nil {
		return Result{}, fmt.Errorf("decode MusicBrainz artist search: %w", err)
	}
	releaseMatches := map[string][]ReleaseHint{}
	for _, hint := range request.Hints.Releases {
		releaseQuery := `releasegroup:"` + escapeLucene(hint.Title) + `"`
		releasePayload, e := client.Search(ctx, "release_group", releaseQuery, 25, 0)
		if e != nil {
			return Result{}, e
		}
		if releasePayload.StatusCode != http.StatusOK {
			return Result{}, &providers.StatusError{Provider: "musicbrainz", StatusCode: releasePayload.StatusCode}
		}
		var releases mbReleaseSearch
		if e := json.Unmarshal(releasePayload.Body, &releases); e != nil {
			return Result{}, e
		}
		for _, group := range releases.ReleaseGroups {
			groupTitle, hintTitle := normalizedText(group.Title), normalizedText(hint.Title)
			if groupTitle != hintTitle && (len([]rune(hintTitle)) < 4 || !strings.Contains(groupTitle, hintTitle)) {
				continue
			}
			if hint.Year > 0 && abs(releaseYear(group.FirstReleaseDate)-hint.Year) > 2 {
				continue
			}
			for _, credit := range group.ArtistCredit {
				if credit.Artist.ID != "" {
					id := strings.ToLower(credit.Artist.ID)
					releaseMatches[id] = appendUniqueReleaseHint(releaseMatches[id], hint)
				}
			}
		}
	}
	candidates := make([]Candidate, 0, len(source.Artists))
	for _, value := range source.Artists {
		id := strings.ToLower(value.ID)
		aliases := make([]string, 0, len(value.Aliases))
		for _, alias := range value.Aliases {
			if strings.TrimSpace(alias.Name) != "" {
				aliases = append(aliases, alias.Name)
			}
		}
		aliases = cleanSorted(aliases)
		display := Display{Name: value.Name, SortName: value.SortName, Disambiguation: value.Disambiguation, Type: normalizeType(value.Type), Country: strings.ToUpper(value.Country), BeginDate: value.LifeSpan.Begin, EndDate: value.LifeSpan.End, Ended: value.LifeSpan.Ended, Aliases: aliases}
		if value.Area != nil {
			display.Area = value.Area.Name
		}
		candidate := Candidate{ProviderScore: value.Score, Identity: ExternalID{Provider: "musicbrainz", Namespace: "artist", Value: id}, Display: display, MatchedReleases: releaseMatches[id], Resolution: Resolution{Kind: KindArtist, Provider: "musicbrainz", Namespace: "artist", Value: id}}
		scoreCandidate(request, &candidate)
		var entityID string
		e := s.runtime.DB.QueryRow(ctx, `SELECT entity_id FROM external_id_claims WHERE entity_kind='artist' AND provider='musicbrainz' AND namespace='artist' AND normalized_value=$1 AND state='accepted'`, id).Scan(&entityID)
		if e == nil {
			candidate.ExistingEntityID = entityID
		} else if e != pgx.ErrNoRows {
			return Result{}, e
		}
		candidates = append(candidates, candidate)
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Confidence != candidates[j].Confidence {
			return candidates[i].Confidence > candidates[j].Confidence
		}
		if candidates[i].ProviderScore != candidates[j].ProviderScore {
			return candidates[i].ProviderScore > candidates[j].ProviderScore
		}
		return candidates[i].Identity.Value < candidates[j].Identity.Value
	})
	if len(candidates) > request.Limit {
		candidates = candidates[:request.Limit]
	}
	for i := range candidates {
		candidates[i].Rank = i + 1
	}
	result := Result{SchemaVersion: SchemaVersion, Kind: KindArtist, Query: request.Query, Status: "completed", Recommendation: recommendation(candidates), Candidates: candidates, Providers: []string{"musicbrainz"}, ObservedAt: time.Now().UTC()}
	return result, nil
}

func NormalizeRequest(request Request) Request {
	request.Kind = strings.ToLower(strings.TrimSpace(request.Kind))
	request.Query = strings.TrimSpace(request.Query)
	if request.Limit < 1 || request.Limit > 25 {
		request.Limit = 10
	}
	request.Hints.Country = strings.ToUpper(strings.TrimSpace(request.Hints.Country))
	request.Hints.Area = strings.TrimSpace(request.Hints.Area)
	request.Hints.Type = normalizeType(request.Hints.Type)
	request.Hints.BeginDate = strings.TrimSpace(request.Hints.BeginDate)
	request.Hints.EndDate = strings.TrimSpace(request.Hints.EndDate)
	request.Hints.Aliases = cleanSorted(request.Hints.Aliases)
	releases := make([]ReleaseHint, 0, len(request.Hints.Releases))
	seen := map[string]bool{}
	for _, hint := range request.Hints.Releases {
		hint.Title = strings.TrimSpace(hint.Title)
		hint.Type = normalizeType(hint.Type)
		key := normalizedText(hint.Title) + ":" + strconv.Itoa(hint.Year) + ":" + hint.Type
		if hint.Title != "" && !seen[key] {
			seen[key] = true
			releases = append(releases, hint)
		}
	}
	sort.Slice(releases, func(i, j int) bool {
		if releases[i].Title != releases[j].Title {
			return releases[i].Title < releases[j].Title
		}
		return releases[i].Year < releases[j].Year
	})
	request.Hints.Releases = releases
	return request
}
func scoreCandidate(request Request, candidate *Candidate) {
	score := float64(candidate.ProviderScore) / 100 * .22
	candidate.Evidence = append(candidate.Evidence, Evidence{Field: "provider_score", Outcome: "support", Weight: round(float64(candidate.ProviderScore) / 100 * .22), Detail: fmt.Sprintf("MusicBrainz score %d/100", candidate.ProviderScore)})
	query := normalizedText(request.Query)
	name := normalizedText(candidate.Display.Name)
	nameSimilarity := similarity(query, name)
	nameWeight := nameSimilarity * .38
	outcome := "fuzzy"
	if query == name {
		outcome = "exact"
		nameWeight = .38
	} else {
		for _, alias := range candidate.Display.Aliases {
			if query == normalizedText(alias) {
				outcome = "exact_alias"
				nameWeight = .35
				break
			}
		}
	}
	score += nameWeight
	candidate.Evidence = append(candidate.Evidence, Evidence{Field: "name", Outcome: outcome, Weight: round(nameWeight), Detail: candidate.Display.Name})
	if country := request.Hints.Country; country != "" {
		weight := -.06
		outcome := "mismatch"
		if country == candidate.Display.Country {
			weight = .11
			outcome = "exact"
		}
		score += weight
		candidate.Evidence = append(candidate.Evidence, Evidence{Field: "country", Outcome: outcome, Weight: weight, Detail: candidate.Display.Country})
	}
	if kind := request.Hints.Type; kind != "" {
		weight := -.04
		outcome := "mismatch"
		if kind == candidate.Display.Type {
			weight = .08
			outcome = "exact"
		}
		score += weight
		candidate.Evidence = append(candidate.Evidence, Evidence{Field: "type", Outcome: outcome, Weight: weight, Detail: candidate.Display.Type})
	}
	if begin := request.Hints.BeginDate; begin != "" {
		weight := -.04
		outcome := "mismatch"
		if begin == candidate.Display.BeginDate {
			weight = .1
			outcome = "exact"
		} else if len(begin) >= 4 && len(candidate.Display.BeginDate) >= 4 && begin[:4] == candidate.Display.BeginDate[:4] {
			weight = .06
			outcome = "year_match"
		}
		score += weight
		candidate.Evidence = append(candidate.Evidence, Evidence{Field: "begin_date", Outcome: outcome, Weight: weight, Detail: candidate.Display.BeginDate})
	}
	if area := request.Hints.Area; area != "" {
		weight, outcome := -.03, "mismatch"
		if normalizedText(area) == normalizedText(candidate.Display.Area) {
			weight, outcome = .07, "exact"
		}
		score += weight
		candidate.Evidence = append(candidate.Evidence, Evidence{Field: "area", Outcome: outcome, Weight: weight, Detail: candidate.Display.Area})
	}
	if end := request.Hints.EndDate; end != "" {
		weight, outcome := -.03, "mismatch"
		if end == candidate.Display.EndDate {
			weight, outcome = .07, "exact"
		} else if len(end) >= 4 && len(candidate.Display.EndDate) >= 4 && end[:4] == candidate.Display.EndDate[:4] {
			weight, outcome = .04, "year_match"
		}
		score += weight
		candidate.Evidence = append(candidate.Evidence, Evidence{Field: "end_date", Outcome: outcome, Weight: weight, Detail: candidate.Display.EndDate})
	}
	if len(request.Hints.Aliases) > 0 {
		matched := 0
		names := append([]string{candidate.Display.Name}, candidate.Display.Aliases...)
		for _, hint := range request.Hints.Aliases {
			for _, name := range names {
				if normalizedText(hint) == normalizedText(name) {
					matched++
					break
				}
			}
		}
		weight := .1 * float64(matched) / float64(len(request.Hints.Aliases))
		score += weight
		candidate.Evidence = append(candidate.Evidence, Evidence{Field: "aliases", Outcome: fmt.Sprintf("%d_of_%d", matched, len(request.Hints.Aliases)), Weight: round(weight)})
	}
	if count := len(request.Hints.Releases); count > 0 {
		weight := .25 * float64(len(candidate.MatchedReleases)) / float64(count)
		score += weight
		candidate.Evidence = append(candidate.Evidence, Evidence{Field: "releases", Outcome: fmt.Sprintf("%d_of_%d", len(candidate.MatchedReleases), count), Weight: round(weight)})
	}
	candidate.Confidence = round(math.Max(0, math.Min(.99, score)))
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
func recommendation(values []Candidate) string {
	if len(values) == 0 {
		return "no_match"
	}
	margin := values[0].Confidence
	if len(values) > 1 {
		margin -= values[1].Confidence
	}
	if values[0].Confidence >= .85 && margin >= .12 {
		return "strong_match"
	}
	if values[0].Confidence >= .65 && margin >= .08 {
		return "likely_match"
	}
	return "ambiguous"
}
func normalizedText(value string) string {
	var out strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(value)) {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			out.WriteRune(r)
		}
	}
	return out.String()
}
func similarity(a, b string) float64 {
	left, right := []rune(a), []rune(b)
	if len(left) == 0 || len(right) == 0 {
		return 0
	}
	previous := make([]int, len(right)+1)
	for j := range previous {
		previous[j] = j
	}
	for i, lr := range left {
		current := make([]int, len(right)+1)
		current[0] = i + 1
		for j, rr := range right {
			cost := 1
			if lr == rr {
				cost = 0
			}
			current[j+1] = min(current[j]+1, previous[j+1]+1, previous[j]+cost)
		}
		previous = current
	}
	distance := previous[len(right)]
	return math.Max(0, 1-float64(distance)/float64(max(len(left), len(right))))
}
func cleanSorted(values []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		key := normalizedText(value)
		if key != "" && !seen[key] {
			seen[key] = true
			out = append(out, value)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
func appendUniqueReleaseHint(values []ReleaseHint, hint ReleaseHint) []ReleaseHint {
	for _, existing := range values {
		if normalizedText(existing.Title) == normalizedText(hint.Title) && existing.Year == hint.Year && existing.Type == hint.Type {
			return values
		}
	}
	return append(values, hint)
}
func normalizeType(value string) string {
	return strings.NewReplacer(" ", "_", "-", "_").Replace(strings.ToLower(strings.TrimSpace(value)))
}
func escapeLucene(value string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value)
}
func releaseYear(value string) int {
	if len(value) < 4 {
		return 0
	}
	year, _ := strconv.Atoi(value[:4])
	return year
}
func abs(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
func round(value float64) float64 { return math.Round(value*1000) / 1000 }

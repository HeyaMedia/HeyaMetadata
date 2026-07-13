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
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/kitsu"
	"github.com/jackc/pgx/v5"
)

type kitsuMangaSearch struct {
	Data []struct {
		ID         string `json:"id"`
		Attributes struct {
			CanonicalTitle string            `json:"canonicalTitle"`
			Titles         map[string]string `json:"titles"`
			StartDate      string            `json:"startDate"`
			Status         string            `json:"status"`
			Subtype        string            `json:"subtype"`
			PopularityRank int               `json:"popularityRank"`
		} `json:"attributes"`
	} `json:"data"`
}

func (s *Service) DiscoverManga(ctx context.Context, request Request, jobID int64) (Result, error) {
	request = NormalizeRequest(request)
	if request.Kind != KindManga || request.Query == "" {
		return Result{}, fmt.Errorf("manga discovery requires kind manga and a query")
	}
	base := kitsu.New(s.runtime.Config.Providers.Kitsu)
	resolver, err := providercache.New(s.runtime, "kitsu-manga-discovery/v1", base.Capability().RawRetention, base.Capability().ResponseCache, jobID)
	if err != nil {
		return Result{}, err
	}
	payload, err := kitsu.NewCached(s.runtime.Config.Providers.Kitsu, resolver).Search(ctx, request.Query, max(request.Limit*2, 10))
	if err != nil {
		return Result{}, err
	}
	if payload.StatusCode != http.StatusOK {
		return Result{}, &providers.StatusError{Provider: "kitsu", StatusCode: payload.StatusCode}
	}
	var source kitsuMangaSearch
	if err = json.Unmarshal(payload.Body, &source); err != nil {
		return Result{}, fmt.Errorf("decode Kitsu manga search: %w", err)
	}
	candidates := []Candidate{}
	for index, value := range source.Data {
		a := value.Attributes
		if value.ID == "" || a.CanonicalTitle == "" || strings.EqualFold(a.Subtype, "novel") {
			continue
		}
		original := firstNonempty(a.Titles["ja_jp"], a.Titles["en_jp"])
		c := Candidate{ProviderScore: max(1, 100-index*4), Identity: ExternalID{Provider: "kitsu", Namespace: "manga", Value: value.ID}, Display: Display{Title: a.CanonicalTitle, OriginalTitle: original, Type: a.Subtype, Year: releaseYear(a.StartDate), Date: a.StartDate, Status: a.Status, Popularity: float64(a.PopularityRank)}, Resolution: Resolution{Kind: KindManga, Provider: "kitsu", Namespace: "manga", Value: value.ID}}
		scoreManga(request, &c)
		var entityID string
		err = s.runtime.DB.QueryRow(ctx, `SELECT entity_id FROM external_id_claims WHERE entity_kind='manga' AND provider='kitsu' AND namespace='manga' AND normalized_value=$1 AND state='accepted'`, value.ID).Scan(&entityID)
		if err == nil {
			c.ExistingEntityID = entityID
		} else if err != pgx.ErrNoRows {
			return Result{}, err
		}
		candidates = append(candidates, c)
	}
	sortCandidates(candidates)
	if len(candidates) > request.Limit {
		candidates = candidates[:request.Limit]
	}
	for i := range candidates {
		candidates[i].Rank = i + 1
	}
	return Result{SchemaVersion: SchemaVersion, Kind: KindManga, Query: request.Query, Status: "completed", Recommendation: recommendation(candidates), Candidates: candidates, Providers: []string{"kitsu"}, ObservedAt: time.Now().UTC()}, nil
}

func scoreManga(r Request, c *Candidate) {
	score := float64(c.ProviderScore) / 100 * .15
	c.Evidence = append(c.Evidence, Evidence{Field: "provider_rank", Outcome: "support", Weight: round(score)})
	q, title := normalizedText(r.Query), normalizedText(c.Display.Title)
	w, outcome := similarity(q, title)*.55, "fuzzy"
	if q == title {
		w, outcome = .55, "exact"
	}
	score += w
	c.Evidence = append(c.Evidence, Evidence{Field: "title", Outcome: outcome, Weight: round(w), Detail: c.Display.Title})
	if normalizedText(r.Query) == normalizedText(c.Display.OriginalTitle) && c.Display.OriginalTitle != "" {
		score += .12
		c.Evidence = append(c.Evidence, Evidence{Field: "original_title", Outcome: "exact", Weight: .12, Detail: c.Display.OriginalTitle})
	}
	if r.Hints.Year > 0 {
		delta := abs(r.Hints.Year - c.Display.Year)
		w = -.06
		outcome = "mismatch"
		if delta == 0 {
			w, outcome = .14, "exact"
		} else if delta <= 1 {
			w, outcome = .06, "near"
		}
		score += w
		c.Evidence = append(c.Evidence, Evidence{Field: "year", Outcome: outcome, Weight: w, Detail: strconv.Itoa(c.Display.Year)})
	}
	c.Confidence = round(math.Max(0, math.Min(.99, score)))
	setMatch(c)
}
func firstNonempty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

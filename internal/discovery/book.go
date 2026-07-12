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
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/openlibrary"
	"github.com/jackc/pgx/v5"
)

type openLibrarySearch struct {
	Docs []struct {
		Key              string   `json:"key"`
		Title            string   `json:"title"`
		Subtitle         string   `json:"subtitle"`
		AuthorKey        []string `json:"author_key"`
		AuthorName       []string `json:"author_name"`
		FirstPublishYear int      `json:"first_publish_year"`
		EditionCount     int      `json:"edition_count"`
		ISBN             []string `json:"isbn"`
		Language         []string `json:"language"`
		Subject          []string `json:"subject"`
		RatingsAverage   float64  `json:"ratings_average"`
		RatingsCount     int      `json:"ratings_count"`
	} `json:"docs"`
}

func (s *Service) DiscoverBook(ctx context.Context, request Request, jobID int64) (Result, error) {
	request = NormalizeRequest(request)
	if request.Kind != KindBookWork || request.Query == "" {
		return Result{}, fmt.Errorf("book discovery requires kind book_work and a query")
	}
	base := openlibrary.New(s.runtime.Config.Providers.OpenLibrary)
	resolver, err := providercache.New(s.runtime, "openlibrary-book-discovery/v1", base.Capability().RawRetention, base.Capability().ResponseCache, jobID)
	if err != nil {
		return Result{}, err
	}
	payload, err := openlibrary.NewCached(s.runtime.Config.Providers.OpenLibrary, resolver).Search(ctx, request.Query, max(request.Limit*3, 20))
	if err != nil {
		return Result{}, err
	}
	if payload.StatusCode != http.StatusOK {
		return Result{}, &providers.StatusError{Provider: "openlibrary", StatusCode: payload.StatusCode}
	}
	var source openLibrarySearch
	if err = json.Unmarshal(payload.Body, &source); err != nil {
		return Result{}, err
	}
	candidates := []Candidate{}
	for index, value := range source.Docs {
		key := strings.ToUpper(strings.TrimPrefix(value.Key, "/works/"))
		if key == "" || value.Title == "" {
			continue
		}
		c := Candidate{ProviderScore: max(1, 100-index*3), Identity: ExternalID{Provider: "openlibrary", Namespace: "work", Value: key}, Display: Display{Title: value.Title, Year: value.FirstPublishYear, Authors: value.AuthorName, EditionCount: value.EditionCount, ISBNs: value.ISBN, Languages: value.Language}, Resolution: Resolution{Kind: KindBookWork, Provider: "openlibrary", Namespace: "work", Value: key}}
		if len(c.Display.ISBNs) > 20 {
			c.Display.ISBNs = c.Display.ISBNs[:20]
		}
		if len(c.Display.Languages) > 20 {
			c.Display.Languages = c.Display.Languages[:20]
		}
		scoreBook(request, &c)
		var id string
		err = s.runtime.DB.QueryRow(ctx, `SELECT entity_id FROM external_id_claims WHERE entity_kind='book_work'AND provider='openlibrary'AND namespace='work'AND normalized_value=$1 AND state='accepted'`, key).Scan(&id)
		if err == nil {
			c.ExistingEntityID = id
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
	return Result{SchemaVersion: SchemaVersion, Kind: KindBookWork, Query: request.Query, Status: "completed", Recommendation: recommendation(candidates), Candidates: candidates, Providers: []string{"openlibrary"}, ObservedAt: time.Now().UTC()}, nil
}
func scoreBook(r Request, c *Candidate) {
	score := float64(c.ProviderScore) / 100 * .15
	c.Evidence = append(c.Evidence, Evidence{Field: "provider_rank", Outcome: "support", Weight: round(score)})
	q := normalizedText(r.Query)
	title := normalizedText(c.Display.Title)
	w, o := similarity(q, title)*.45, "fuzzy"
	if q == title {
		w, o = .45, "exact"
	}
	score += w
	c.Evidence = append(c.Evidence, Evidence{Field: "title", Outcome: o, Weight: round(w), Detail: c.Display.Title})
	if r.Hints.Year > 0 {
		w, o = -.06, "mismatch"
		d := abs(r.Hints.Year - c.Display.Year)
		if d == 0 {
			w, o = .14, "exact"
		} else if d <= 1 {
			w, o = .06, "near"
		}
		score += w
		c.Evidence = append(c.Evidence, Evidence{Field: "first_publish_year", Outcome: o, Weight: w, Detail: strconv.Itoa(c.Display.Year)})
	}
	authors := r.Hints.Authors
	if len(authors) == 0 {
		authors = r.Hints.Artists
	}
	if len(authors) > 0 {
		matched := 0
		for _, hint := range authors {
			for _, got := range c.Display.Authors {
				if normalizedText(hint) == normalizedText(got) {
					matched++
					break
				}
			}
		}
		w = .18 * float64(matched) / float64(len(authors))
		score += w
		c.Evidence = append(c.Evidence, Evidence{Field: "authors", Outcome: fmt.Sprintf("%d_of_%d", matched, len(authors)), Weight: round(w), Detail: strings.Join(c.Display.Authors, ", ")})
	}
	if len(r.Hints.ISBNs) > 0 {
		matched := 0
		for _, hint := range r.Hints.ISBNs {
			for _, got := range c.Display.ISBNs {
				if normalizeBookID(hint) == normalizeBookID(got) {
					matched++
					break
				}
			}
		}
		w = .2 * float64(matched) / float64(len(r.Hints.ISBNs))
		score += w
		c.Evidence = append(c.Evidence, Evidence{Field: "isbns", Outcome: fmt.Sprintf("%d_of_%d", matched, len(r.Hints.ISBNs)), Weight: round(w)})
	}
	c.Confidence = round(math.Max(0, math.Min(.99, score)))
	setMatch(c)
}
func normalizeBookID(v string) string {
	return strings.ToUpper(strings.NewReplacer("-", "", " ", "").Replace(v))
}

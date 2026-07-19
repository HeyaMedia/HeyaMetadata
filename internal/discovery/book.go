package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
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

type openLibraryBookDoc struct {
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
}

type openLibrarySearch struct {
	Docs []openLibraryBookDoc `json:"docs"`
}

type openLibraryBookSearcher interface {
	Search(context.Context, string, int) (providers.Payload, error)
	SearchByTitleAuthor(context.Context, string, string, int) (providers.Payload, error)
}

func (s *Service) DiscoverBook(ctx context.Context, request Request, jobID int64) (Result, error) {
	request = NormalizeRequest(request)
	if request.Kind != KindBookWork && request.Kind != KindMangaVolume && request.Kind != KindComicVolume || request.Query == "" {
		return Result{}, fmt.Errorf("publication discovery requires kind book_work, manga_volume, or comic_volume and a query")
	}
	base := openlibrary.New(s.runtime.Config.Providers.OpenLibrary)
	resolver, err := providercache.New(s.runtime, "openlibrary-book-discovery/v1", base.Capability().RawRetention, base.Capability().ResponseCache, jobID)
	if err != nil {
		return Result{}, err
	}
	client := openlibrary.NewCached(s.runtime.Config.Providers.OpenLibrary, resolver)
	searchLimit := max(request.Limit*3, 20)
	docs, warnings, err := searchOpenLibraryBookDocs(ctx, client, request, searchLimit)
	if err != nil {
		return Result{}, err
	}
	candidates := []Candidate{}
	for _, value := range docs {
		if !publicationSubjectsMatch(request.Kind, value.Subject) {
			continue
		}
		key, valid := openlibrary.CanonicalKey(value.Key)
		if !valid || !strings.HasSuffix(key, "W") || strings.TrimSpace(value.Title) == "" {
			continue
		}
		index := len(candidates)
		c := Candidate{ProviderScore: max(1, 100-index*3), Identity: ExternalID{Provider: "openlibrary", Namespace: "work", Value: key}, Display: Display{Title: value.Title, Type: request.Kind, Year: value.FirstPublishYear, Authors: value.AuthorName, EditionCount: value.EditionCount, ISBNs: value.ISBN, Languages: value.Language}, Resolution: Resolution{Kind: request.Kind, Provider: "openlibrary", Namespace: "work", Value: key}}
		if len(c.Display.ISBNs) > 20 {
			c.Display.ISBNs = c.Display.ISBNs[:20]
		}
		if len(c.Display.Languages) > 20 {
			c.Display.Languages = c.Display.Languages[:20]
		}
		scoreBook(request, &c)
		var id string
		err = s.runtime.DB.QueryRow(ctx, `SELECT entity_id FROM external_id_claims WHERE entity_kind=$1 AND provider='openlibrary'AND namespace='work'AND normalized_value=$2 AND state='accepted'`, request.Kind, key).Scan(&id)
		if err == nil {
			c.ExistingEntityID = id
		} else if err != pgx.ErrNoRows {
			return Result{}, err
		}
		candidates = append(candidates, c)
	}
	sortCandidates(candidates)
	recommended, candidates := presentCandidates(candidates, request.Limit)
	return Result{SchemaVersion: SchemaVersion, Kind: request.Kind, Query: request.Query, Status: "completed", Recommendation: recommended, Candidates: candidates, Providers: []string{"openlibrary"}, ObservedAt: time.Now().UTC(), Warnings: warnings}, nil
}

func searchOpenLibraryBookDocs(ctx context.Context, client openLibraryBookSearcher, request Request, limit int) ([]openLibraryBookDoc, []string, error) {
	payload, err := client.Search(ctx, request.Query, limit)
	if err != nil {
		return nil, nil, err
	}
	if payload.StatusCode != http.StatusOK {
		return nil, nil, &providers.StatusError{Provider: "openlibrary", StatusCode: payload.StatusCode}
	}
	var broad openLibrarySearch
	if err := json.Unmarshal(payload.Body, &broad); err != nil {
		return nil, nil, fmt.Errorf("decode Open Library broad book search: %w", err)
	}
	authors := bookHintAuthors(request)
	if len(authors) == 0 || bookSearchHasAllAuthors(broad.Docs, authors) {
		return mergeOpenLibraryBookDocs(broad.Docs), nil, nil
	}

	author := mostDistinctiveBookAuthor(authors)
	structured, structuredErr := client.SearchByTitleAuthor(ctx, request.Query, author, limit)
	if structuredErr != nil {
		return mergeOpenLibraryBookDocs(broad.Docs), []string{warnStructuredBookSearch(ctx, author, structuredErr)}, nil
	}
	if structured.StatusCode != http.StatusOK {
		statusErr := &providers.StatusError{Provider: "openlibrary", StatusCode: structured.StatusCode}
		return mergeOpenLibraryBookDocs(broad.Docs), []string{warnStructuredBookSearch(ctx, author, statusErr)}, nil
	}
	var qualified openLibrarySearch
	if err := json.Unmarshal(structured.Body, &qualified); err != nil {
		decodeErr := fmt.Errorf("decode Open Library structured book search: %w", err)
		return mergeOpenLibraryBookDocs(broad.Docs), []string{warnStructuredBookSearch(ctx, author, decodeErr)}, nil
	}
	return mergeOpenLibraryBookDocs(qualified.Docs, broad.Docs), nil, nil
}

func warnStructuredBookSearch(ctx context.Context, author string, err error) string {
	slog.WarnContext(ctx, "supplemental Open Library structured book discovery failed", "author", author, "error", err)
	return "openlibrary.structured_search: " + err.Error()
}

func bookHintAuthors(request Request) []string {
	if len(request.Hints.Authors) > 0 {
		return request.Hints.Authors
	}
	return request.Hints.Artists
}

func mostDistinctiveBookAuthor(authors []string) string {
	best := ""
	for _, author := range authors {
		if len([]rune(normalizedText(author))) > len([]rune(normalizedText(best))) {
			best = author
		}
	}
	return best
}

func bookSearchHasAllAuthors(docs []openLibraryBookDoc, hints []string) bool {
	for _, doc := range docs {
		key, valid := openlibrary.CanonicalKey(doc.Key)
		if !valid || !strings.HasSuffix(key, "W") || strings.TrimSpace(doc.Title) == "" {
			continue
		}
		matched := 0
		for _, hint := range hints {
			for _, author := range doc.AuthorName {
				if normalizedText(hint) == normalizedText(author) {
					matched++
					break
				}
			}
		}
		if matched == len(hints) {
			return true
		}
	}
	return false
}

func mergeOpenLibraryBookDocs(groups ...[]openLibraryBookDoc) []openLibraryBookDoc {
	indexes := make(map[string]int)
	merged := make([]openLibraryBookDoc, 0)
	for _, docs := range groups {
		for _, doc := range docs {
			key, valid := openlibrary.CanonicalKey(doc.Key)
			if !valid || !strings.HasSuffix(key, "W") {
				continue
			}
			doc.Key = "/works/" + key
			if index, exists := indexes[key]; exists {
				merged[index] = mergeOpenLibraryBookDoc(merged[index], doc)
				continue
			}
			indexes[key] = len(merged)
			merged = append(merged, doc)
		}
	}
	return merged
}

func mergeOpenLibraryBookDoc(primary, supplemental openLibraryBookDoc) openLibraryBookDoc {
	if strings.TrimSpace(primary.Title) == "" {
		primary.Title = supplemental.Title
	}
	if strings.TrimSpace(primary.Subtitle) == "" {
		primary.Subtitle = supplemental.Subtitle
	}
	primary.AuthorKey = mergeBookStrings(primary.AuthorKey, supplemental.AuthorKey, normalizedText)
	primary.AuthorName = mergeBookStrings(primary.AuthorName, supplemental.AuthorName, normalizedText)
	primary.ISBN = mergeBookStrings(primary.ISBN, supplemental.ISBN, normalizeBookID)
	primary.Language = mergeBookStrings(primary.Language, supplemental.Language, normalizedText)
	primary.Subject = mergeBookStrings(primary.Subject, supplemental.Subject, normalizedText)
	if primary.FirstPublishYear == 0 {
		primary.FirstPublishYear = supplemental.FirstPublishYear
	}
	primary.EditionCount = max(primary.EditionCount, supplemental.EditionCount)
	if supplemental.RatingsCount > primary.RatingsCount {
		primary.RatingsAverage = supplemental.RatingsAverage
		primary.RatingsCount = supplemental.RatingsCount
	}
	return primary
}

func mergeBookStrings(primary, supplemental []string, key func(string) string) []string {
	seen := make(map[string]bool, len(primary)+len(supplemental))
	result := make([]string, 0, len(primary)+len(supplemental))
	for _, values := range [][]string{primary, supplemental} {
		for _, value := range values {
			identity := key(value)
			if identity == "" || seen[identity] {
				continue
			}
			seen[identity] = true
			result = append(result, value)
		}
	}
	return result
}

func publicationSubjectsMatch(kind string, subjects []string) bool {
	if kind == KindBookWork {
		return true
	}
	manga := false
	comic := false
	for _, subject := range subjects {
		value := strings.ToLower(subject)
		if strings.Contains(value, "manga") || strings.Contains(value, "manhwa") || strings.Contains(value, "manhua") {
			manga = true
		}
		if strings.Contains(value, "comic") || strings.Contains(value, "graphic novel") || strings.Contains(value, "sequential art") {
			comic = true
		}
	}
	if kind == KindMangaVolume {
		return manga
	}
	return comic && !manga
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
		w, o = 0, "mismatch"
		d := abs(r.Hints.Year - c.Display.Year)
		if d == 0 {
			w, o = .1, "exact"
		} else if d <= 1 {
			w, o = .04, "near"
		}
		score += w
		c.Evidence = append(c.Evidence, Evidence{Field: "first_publish_year", Outcome: o, Weight: round(w), Detail: strconv.Itoa(c.Display.Year)})
	}
	authors := bookHintAuthors(r)
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
		// Author evidence is identity-bearing for a work, while a folder year
		// often describes the audiobook edition rather than first publication.
		w = .25 * float64(matched) / float64(len(authors))
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

package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/providercache"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/openopus"
	"github.com/jackc/pgx/v5"
)

type openOpusOmniResponse struct {
	Status struct {
		Success string `json:"success"`
		Rows    int    `json:"rows"`
	} `json:"status"`
	Results []struct {
		Composer struct {
			ID           string `json:"id"`
			Name         string `json:"name"`
			CompleteName string `json:"complete_name"`
			Epoch        string `json:"epoch"`
		} `json:"composer"`
		Work struct {
			ID               string `json:"id"`
			Title            string `json:"title"`
			Subtitle         string `json:"subtitle"`
			Genre            string `json:"genre"`
			Popular          string `json:"popular"`
			Recommended      string `json:"recommended"`
			Catalogue        string `json:"catalogue"`
			CatalogueNumber  string `json:"catalogue_number"`
			AdditionalNumber string `json:"additional_number"`
		} `json:"work"`
	} `json:"results"`
	Next int `json:"next"`
}

// DiscoverMusicalWork searches composed works without asserting that similarly
// titled works share an identity. Composer and catalogue hints are deliberately
// weighted above Open Opus popularity signals.
func (s *Service) DiscoverMusicalWork(ctx context.Context, request Request, jobID int64) (Result, error) {
	request = NormalizeRequest(request)
	if request.Kind != KindMusicalWork {
		return Result{}, fmt.Errorf("musical-work discovery requires kind musical_work")
	}
	if request.Query == "" {
		return Result{}, fmt.Errorf("discovery query is required")
	}
	base := openopus.New(s.runtime.Config.Providers.OpenOpus)
	resolver, err := providercache.New(s.runtime, "openopus-musical-work-discovery/v1", base.Capability().RawRetention, base.Capability().ResponseCache, jobID)
	if err != nil {
		return Result{}, err
	}
	client := openopus.NewCached(s.runtime.Config.Providers.OpenOpus, resolver)
	target := min(100, max(20, request.Limit*4))
	values := make([]openOpusOmniResponse, 0, 5)
	for offset := 0; lenOpenOpusResults(values) < target; {
		payload, searchErr := client.OmniSearch(ctx, request.Query, offset)
		if searchErr != nil {
			return Result{}, searchErr
		}
		if payload.StatusCode != http.StatusOK {
			return Result{}, &providers.StatusError{Provider: "openopus", StatusCode: payload.StatusCode}
		}
		var response openOpusOmniResponse
		if err := json.Unmarshal(payload.Body, &response); err != nil {
			return Result{}, fmt.Errorf("decode Open Opus omnisearch: %w", err)
		}
		if !strings.EqualFold(response.Status.Success, "true") {
			if response.Status.Rows == 0 {
				break
			}
			return Result{}, fmt.Errorf("Open Opus omnisearch failed")
		}
		values = append(values, response)
		if len(response.Results) == 0 || response.Next <= offset {
			break
		}
		offset = response.Next
	}

	candidates := make([]Candidate, 0, lenOpenOpusResults(values))
	seen := map[string]bool{}
	for _, response := range values {
		for _, value := range response.Results {
			workID := strings.TrimSpace(value.Work.ID)
			title := strings.TrimSpace(value.Work.Title)
			if workID == "" || title == "" || seen[workID] {
				continue
			}
			seen[workID] = true
			composerName := firstDiscoveryValue(value.Composer.CompleteName, value.Composer.Name)
			catalogue := openOpusCatalogue(value.Work.Catalogue, value.Work.CatalogueNumber, value.Work.AdditionalNumber)
			providerScore := 0
			if openOpusTrue(value.Work.Popular) {
				providerScore += 65
			}
			if openOpusTrue(value.Work.Recommended) {
				providerScore += 35
			}
			candidate := Candidate{
				ProviderScore: providerScore,
				Identity:      ExternalID{Provider: "openopus", Namespace: "work", Value: workID},
				Display: Display{
					Title:          title,
					Disambiguation: strings.TrimSpace(value.Work.Subtitle),
					Type:           strings.TrimSpace(value.Work.Genre),
					Area:           strings.TrimSpace(value.Composer.Epoch),
					Artists:        []ArtistDisplay{{ID: strings.TrimSpace(value.Composer.ID), Name: composerName}},
					Catalogue:      catalogue,
				},
				Resolution: Resolution{Kind: KindMusicalWork, Provider: "openopus", Namespace: "work", Value: workID},
			}
			scoreMusicalWorkCandidate(request, &candidate)
			var entityID string
			err := s.runtime.DB.QueryRow(ctx, `SELECT entity_id::text FROM external_id_claims WHERE entity_kind='musical_work' AND provider='openopus' AND namespace='work' AND normalized_value=$1 AND state='accepted'`, workID).Scan(&entityID)
			if err == nil {
				candidate.ExistingEntityID = entityID
			} else if err != pgx.ErrNoRows {
				return Result{}, err
			}
			candidates = append(candidates, candidate)
		}
	}
	sortCandidates(candidates)
	if len(candidates) > request.Limit {
		candidates = candidates[:request.Limit]
	}
	for i := range candidates {
		candidates[i].Rank = i + 1
	}
	return Result{SchemaVersion: SchemaVersion, Kind: KindMusicalWork, Query: request.Query, Status: "completed", Recommendation: recommendation(candidates), Candidates: candidates, Providers: []string{"openopus"}, ObservedAt: time.Now().UTC()}, nil
}

func scoreMusicalWorkCandidate(request Request, candidate *Candidate) {
	query := normalizedText(request.Query)
	title := normalizedText(candidate.Display.Title)
	weight, outcome := similarity(query, title)*.5, "fuzzy"
	if query == title {
		weight, outcome = .5, "exact"
	} else if strings.Contains(title, query) || strings.Contains(query, title) {
		weight, outcome = max(weight, .4), "contains"
	}
	score := weight
	candidate.Evidence = append(candidate.Evidence, Evidence{Field: "title", Outcome: outcome, Weight: round(weight), Detail: candidate.Display.Title})

	if len(request.Hints.Composers) > 0 {
		matched := matchedArtists(request.Hints.Composers, nil, candidate.Display.Artists)
		weight = -.1
		if matched > 0 {
			weight = .3 * float64(matched) / float64(len(request.Hints.Composers))
		}
		score += weight
		candidate.Evidence = append(candidate.Evidence, Evidence{Field: "composers", Outcome: fmt.Sprintf("%d_of_%d", matched, len(request.Hints.Composers)), Weight: round(weight), Detail: candidateComposer(candidate)})
	}
	if len(request.Hints.ComposerIDs) > 0 {
		matched := matchedArtists(nil, request.Hints.ComposerIDs, candidate.Display.Artists)
		weight = -.15
		if matched > 0 {
			weight = .38 * float64(matched) / float64(len(request.Hints.ComposerIDs))
		}
		score += weight
		candidate.Evidence = append(candidate.Evidence, Evidence{Field: "composer_ids", Outcome: fmt.Sprintf("%d_of_%d", matched, len(request.Hints.ComposerIDs)), Weight: round(weight)})
	}
	if request.Hints.Catalogue != "" {
		hint := normalizedText(request.Hints.Catalogue)
		actual := normalizedText(candidate.Display.Catalogue + " " + candidate.Display.Title)
		weight, outcome = -.08, "mismatch"
		if hint != "" && strings.Contains(actual, hint) {
			weight, outcome = .2, "match"
		}
		score += weight
		candidate.Evidence = append(candidate.Evidence, Evidence{Field: "catalogue", Outcome: outcome, Weight: weight, Detail: candidate.Display.Catalogue})
	}
	if request.Hints.Type != "" {
		weight, outcome = -.03, "mismatch"
		if normalizedText(request.Hints.Type) == normalizedText(candidate.Display.Type) {
			weight, outcome = .08, "exact"
		}
		score += weight
		candidate.Evidence = append(candidate.Evidence, Evidence{Field: "genre", Outcome: outcome, Weight: weight, Detail: candidate.Display.Type})
	}
	providerWeight := float64(candidate.ProviderScore) / 100 * .1
	score += providerWeight
	candidate.Evidence = append(candidate.Evidence, Evidence{Field: "provider_prominence", Outcome: "tie_breaker", Weight: round(providerWeight), Detail: fmt.Sprintf("Open Opus score %d/100", candidate.ProviderScore)})
	candidate.Confidence = round(math.Max(0, math.Min(.99, score)))
	setMatch(candidate)
}

func lenOpenOpusResults(values []openOpusOmniResponse) int {
	total := 0
	for _, value := range values {
		total += len(value.Results)
	}
	return total
}

func firstDiscoveryValue(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func openOpusCatalogue(system, number, additional string) string {
	parts := make([]string, 0, 2)
	if value := strings.TrimSpace(strings.TrimSpace(system) + " " + strings.TrimSpace(number)); value != "" {
		parts = append(parts, value)
	}
	if additional = strings.TrimSpace(additional); additional != "" {
		parts = append(parts, additional)
	}
	return strings.Join(parts, " / ")
}

func openOpusTrue(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	return value == "1" || value == "true" || value == "yes"
}

func candidateComposer(candidate *Candidate) string {
	if len(candidate.Display.Artists) == 0 {
		return ""
	}
	return candidate.Display.Artists[0].Name
}

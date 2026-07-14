package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/HeyaMedia/HeyaMetadata/internal/musiccatalog"
	"github.com/HeyaMedia/HeyaMetadata/internal/providercache"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/apple"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/deezer"
	"github.com/jackc/pgx/v5"
)

func (s *Service) discoverStorefrontArtistCandidates(ctx context.Context, request Request, jobID int64) ([]Candidate, []string, error) {
	result := []Candidate{}
	providersUsed := []string{}
	appleCandidates, err := s.discoverAppleArtists(ctx, request, jobID)
	if err == nil {
		result = append(result, appleCandidates...)
		providersUsed = append(providersUsed, "apple")
	}
	deezerCandidates, deezerErr := s.discoverDeezerArtists(ctx, request, jobID)
	if deezerErr == nil {
		result = append(result, deezerCandidates...)
		providersUsed = append(providersUsed, "deezer")
	}
	if err != nil && deezerErr != nil {
		return nil, nil, fmt.Errorf("Apple artist search: %v; Deezer artist search: %w", err, deezerErr)
	}
	return result, providersUsed, nil
}

func (s *Service) discoverAppleArtists(ctx context.Context, request Request, jobID int64) ([]Candidate, error) {
	base := apple.New(s.runtime.Config.Providers.Apple)
	resolver, err := providercache.New(s.runtime, "apple-artist-discovery/v1", base.Capability().RawRetention, base.Capability().ResponseCache, jobID)
	if err != nil {
		return nil, err
	}
	client := apple.NewCached(s.runtime.Config.Providers.Apple, resolver, "")
	payload, err := client.Search(ctx, "artist", request.Query, request.Hints.Country, min(100, max(25, request.Limit*4)))
	if err != nil {
		return nil, err
	}
	if payload.StatusCode != http.StatusOK {
		return nil, &providers.StatusError{Provider: "apple", StatusCode: payload.StatusCode}
	}
	var envelope struct {
		Results []struct {
			WrapperType      string `json:"wrapperType"`
			ArtistID         int64  `json:"artistId"`
			ArtistName       string `json:"artistName"`
			ArtistType       string `json:"artistType"`
			PrimaryGenreName string `json:"primaryGenreName"`
		} `json:"results"`
	}
	if err := json.Unmarshal(payload.Body, &envelope); err != nil {
		return nil, err
	}
	result := []Candidate{}
	seen := map[string]bool{}
	catalogLookups := 0
	for _, hit := range envelope.Results {
		id := strconv.FormatInt(hit.ArtistID, 10)
		if !strings.EqualFold(hit.WrapperType, "artist") || hit.ArtistID < 1 || strings.TrimSpace(hit.ArtistName) == "" || seen[id] {
			continue
		}
		seen[id] = true
		matched := []ReleaseHint{}
		if len(request.Hints.Releases) > 0 && normalizedText(hit.ArtistName) == normalizedText(request.Query) && catalogLookups < 5 {
			catalogLookups++
			payloads, collectErr := client.Collect(ctx, providers.Identifier{Provider: "apple", Namespace: "artist", Value: id})
			if collectErr == nil && len(payloads) > 0 && payloads[0].StatusCode == http.StatusOK {
				catalog, _ := musiccatalog.AppleIdentityCatalog(payloads[0].Body, id)
				matched = matchedCatalogReleaseHints(request.Hints.Releases, catalog)
			}
		}
		providerScore := int(similarity(normalizedText(request.Query), normalizedText(hit.ArtistName))*100 + .5)
		candidate := Candidate{
			ProviderScore:   providerScore,
			Identity:        ExternalID{Provider: "apple", Namespace: "artist", Value: id},
			Display:         Display{Name: hit.ArtistName, Type: normalizeType(hit.ArtistType)},
			MatchedReleases: matched,
			Resolution:      Resolution{Kind: KindArtist, Provider: "apple", Namespace: "artist", Value: id},
		}
		scoreCandidate(request, &candidate)
		if err := s.lookupExistingArtistCandidate(ctx, &candidate); err != nil {
			return nil, err
		}
		result = append(result, candidate)
	}
	return result, nil
}

func (s *Service) discoverDeezerArtists(ctx context.Context, request Request, jobID int64) ([]Candidate, error) {
	base := deezer.New(s.runtime.Config.Providers.Deezer)
	resolver, err := providercache.New(s.runtime, "deezer-artist-discovery/v1", base.Capability().RawRetention, base.Capability().ResponseCache, jobID)
	if err != nil {
		return nil, err
	}
	client := deezer.NewCached(s.runtime.Config.Providers.Deezer, resolver)
	payload, err := client.Search(ctx, "artist", request.Query, min(100, max(25, request.Limit*4)), 0)
	if err != nil {
		return nil, err
	}
	if payload.StatusCode != http.StatusOK {
		return nil, &providers.StatusError{Provider: "deezer", StatusCode: payload.StatusCode}
	}
	var envelope struct {
		Data []struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		} `json:"data"`
	}
	if err := json.Unmarshal(payload.Body, &envelope); err != nil {
		return nil, err
	}
	result := []Candidate{}
	catalogLookups := 0
	for _, hit := range envelope.Data {
		if hit.ID < 1 || strings.TrimSpace(hit.Name) == "" {
			continue
		}
		id := strconv.FormatInt(hit.ID, 10)
		matched := []ReleaseHint{}
		if len(request.Hints.Releases) > 0 && normalizedText(hit.Name) == normalizedText(request.Query) && catalogLookups < 5 {
			catalogLookups++
			albumPayload, collectErr := client.ArtistAlbums(ctx, id, 200, 0)
			if collectErr == nil && albumPayload.StatusCode == http.StatusOK {
				catalog, _, _ := musiccatalog.DeezerIdentityCatalog(albumPayload.Body, id)
				matched = matchedCatalogReleaseHints(request.Hints.Releases, catalog)
			}
		}
		providerScore := int(similarity(normalizedText(request.Query), normalizedText(hit.Name))*100 + .5)
		candidate := Candidate{
			ProviderScore:   providerScore,
			Identity:        ExternalID{Provider: "deezer", Namespace: "artist", Value: id},
			Display:         Display{Name: hit.Name},
			MatchedReleases: matched,
			Resolution:      Resolution{Kind: KindArtist, Provider: "deezer", Namespace: "artist", Value: id},
		}
		scoreCandidate(request, &candidate)
		if err := s.lookupExistingArtistCandidate(ctx, &candidate); err != nil {
			return nil, err
		}
		result = append(result, candidate)
	}
	return result, nil
}

func matchedCatalogReleaseHints(hints []ReleaseHint, catalog []musiccatalog.IdentityRelease) []ReleaseHint {
	result := []ReleaseHint{}
	for _, hint := range hints {
		date := ""
		if hint.Year > 0 {
			date = strconv.Itoa(hint.Year)
		}
		probe := []musiccatalog.IdentityRelease{{Title: hint.Title, Date: date, Kind: hint.Type}}
		if musiccatalog.IdentityCatalogOverlap(probe, catalog) > 0 {
			result = appendUniqueReleaseHint(result, hint)
		}
	}
	return result
}

func (s *Service) lookupExistingArtistCandidate(ctx context.Context, candidate *Candidate) error {
	err := s.runtime.DB.QueryRow(ctx, `SELECT entity_id FROM external_id_claims WHERE entity_kind='artist' AND provider=$1 AND namespace='artist' AND normalized_value=$2 AND state='accepted'`, candidate.Identity.Provider, candidate.Identity.Value).Scan(&candidate.ExistingEntityID)
	if err == pgx.ErrNoRows {
		return nil
	}
	return err
}

package artists

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/HeyaMedia/HeyaMetadata/internal/changelog"
	artistdomain "github.com/HeyaMedia/HeyaMetadata/internal/domains/artist"
	"github.com/HeyaMedia/HeyaMetadata/internal/musiccatalog"
	"github.com/HeyaMedia/HeyaMetadata/internal/providercache"
	"github.com/HeyaMedia/HeyaMetadata/internal/providercredentials"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/apple"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/deezer"
)

// IngestApple establishes an Apple/iTunes artist as a canonical root. A
// catalog-backed match may attach it to an existing artist; an exact name by
// itself never does.
func (s *Service) IngestApple(ctx context.Context, artistID string, riverJobID int64, credentials providercredentials.Credentials) (result Result, returnErr error) {
	artistID = strings.TrimSpace(artistID)
	if err := s.startIngestionRun(ctx, "apple", artistID, riverJobID); err != nil {
		return Result{}, err
	}
	defer s.finishFailedIngestionRun(ctx, riverJobID, &returnErr)
	base := apple.New(s.runtime.Config.Providers.Apple)
	resolver, err := providercache.New(s.runtime, artistdomain.AppleNormalizerVersion, base.Capability().RawRetention, base.Capability().ResponseCache, riverJobID)
	if err != nil {
		return Result{}, err
	}
	client := apple.NewCached(s.runtime.Config.Providers.Apple, resolver, credentials.APIKey("apple"))
	payloads, err := client.Collect(ctx, providers.Identifier{Provider: "apple", Namespace: "artist", Value: artistID})
	if err != nil {
		return Result{}, err
	}
	recorded, err := s.recordPayloads(ctx, payloads, artistdomain.AppleNormalizerVersion, base.Capability(), riverJobID)
	if err != nil {
		return Result{}, err
	}
	if len(recorded) == 0 {
		return Result{}, fmt.Errorf("Apple artist collector returned no observations")
	}
	if recorded[0].Payload.StatusCode != http.StatusOK {
		return Result{}, &providers.StatusError{Provider: "apple", StatusCode: recorded[0].Payload.StatusCode}
	}
	record, err := apple.NormalizeArtist(recorded[0].Payload.Body, artistID, recorded[0].ID, recorded[0].Payload.ObservedAt)
	if err != nil {
		return Result{}, err
	}
	catalog, err := musiccatalog.AppleIdentityCatalog(recorded[0].Payload.Body, artistID)
	if err != nil {
		return Result{}, err
	}
	records, catalogs := s.collectStorefrontCounterpart(ctx, record, catalog, riverJobID, credentials)
	return s.mergeStorefrontArtist(ctx, records, catalogs, riverJobID)
}

// IngestDeezer establishes a Deezer artist as a canonical root. Deezer keeps
// the profile and albums on separate endpoints, so the latter is read only for
// conservative identity reconciliation and the follow-up catalog job.
func (s *Service) IngestDeezer(ctx context.Context, artistID string, riverJobID int64, _ providercredentials.Credentials) (result Result, returnErr error) {
	artistID = strings.TrimSpace(artistID)
	if err := s.startIngestionRun(ctx, "deezer", artistID, riverJobID); err != nil {
		return Result{}, err
	}
	defer s.finishFailedIngestionRun(ctx, riverJobID, &returnErr)
	base := deezer.New(s.runtime.Config.Providers.Deezer)
	resolver, err := providercache.New(s.runtime, artistdomain.DeezerNormalizerVersion, base.Capability().RawRetention, base.Capability().ResponseCache, riverJobID)
	if err != nil {
		return Result{}, err
	}
	client := deezer.NewCached(s.runtime.Config.Providers.Deezer, resolver)
	payloads, err := client.Collect(ctx, providers.Identifier{Provider: "deezer", Namespace: "artist", Value: artistID})
	if err != nil {
		return Result{}, err
	}
	recorded, err := s.recordPayloads(ctx, payloads, artistdomain.DeezerNormalizerVersion, base.Capability(), riverJobID)
	if err != nil {
		return Result{}, err
	}
	if len(recorded) == 0 {
		return Result{}, fmt.Errorf("Deezer artist collector returned no observations")
	}
	if recorded[0].Payload.StatusCode != http.StatusOK {
		return Result{}, &providers.StatusError{Provider: "deezer", StatusCode: recorded[0].Payload.StatusCode}
	}
	record, err := deezer.NormalizeArtist(recorded[0].Payload.Body, recorded[0].ID, recorded[0].Payload.ObservedAt)
	if err != nil {
		return Result{}, err
	}
	catalog, err := collectDeezerIdentityCatalog(ctx, client, artistID)
	if err != nil {
		return Result{}, err
	}
	records, catalogs := s.collectStorefrontCounterpart(ctx, record, catalog, riverJobID, providercredentials.Credentials{})
	return s.mergeStorefrontArtist(ctx, records, catalogs, riverJobID)
}

func collectDeezerIdentityCatalog(ctx context.Context, client *deezer.Client, artistID string) ([]musiccatalog.IdentityRelease, error) {
	result := []musiccatalog.IdentityRelease{}
	for index := 0; ; {
		payload, err := client.ArtistAlbums(ctx, artistID, 200, index)
		if err != nil {
			return nil, err
		}
		if payload.StatusCode == http.StatusNotFound {
			return result, nil
		}
		if payload.StatusCode != http.StatusOK {
			return nil, &providers.StatusError{Provider: "deezer", StatusCode: payload.StatusCode}
		}
		items, total, err := musiccatalog.DeezerIdentityCatalog(payload.Body, artistID)
		if err != nil {
			return nil, err
		}
		result = append(result, items...)
		index += len(items)
		if len(items) == 0 || index >= total {
			return result, nil
		}
	}
}

func (s *Service) mergeStorefrontArtist(ctx context.Context, records []artistdomain.NormalizedRecordV1, catalogs []musiccatalog.IdentityRelease, riverJobID int64) (Result, error) {
	if len(records) == 0 {
		return Result{}, fmt.Errorf("storefront artist merge requires a primary record")
	}
	primary := records[0]
	preferredEntityID, overlap, err := musiccatalog.FindCanonicalArtistByCatalog(ctx, s.runtime, preferredName(primary), catalogs)
	if err != nil {
		return Result{}, err
	}
	if preferredEntityID != "" {
		slog.InfoContext(ctx, "storefront artist catalog converged on canonical artist",
			"provider", primary.ProviderRecord.Provider,
			"provider_artist_id", primary.ProviderRecord.Value,
			"artist_entity_id", preferredEntityID,
			"release_overlap", overlap,
		)
	}
	normalizedIDs := make([]string, 0, len(records))
	for _, record := range records {
		normalizedID, recordErr := s.recordNormalized(ctx, record)
		if recordErr != nil {
			return Result{}, recordErr
		}
		normalizedIDs = append(normalizedIDs, normalizedID)
	}
	result, err := s.merge(ctx, normalizedIDs, records, riverJobID, preferredEntityID)
	if err != nil {
		return Result{}, err
	}
	if err := s.cache(ctx, result); err != nil {
		return Result{}, err
	}
	changelog.SequenceBestEffort(ctx, s.runtime, 100)
	return result, nil
}

func (s *Service) collectStorefrontCounterpart(ctx context.Context, primary artistdomain.NormalizedRecordV1, primaryCatalog []musiccatalog.IdentityRelease, riverJobID int64, credentials providercredentials.Credentials) ([]artistdomain.NormalizedRecordV1, []musiccatalog.IdentityRelease) {
	records := []artistdomain.NormalizedRecordV1{primary}
	catalogs := append([]musiccatalog.IdentityRelease(nil), primaryCatalog...)
	if len(primaryCatalog) < 2 {
		return records, catalogs
	}
	name := preferredName(primary)
	var counterpart artistdomain.NormalizedRecordV1
	var counterpartCatalog []musiccatalog.IdentityRelease
	var overlap int
	var err error
	switch primary.ProviderRecord.Provider {
	case "apple":
		counterpart, counterpartCatalog, overlap, err = s.matchDeezerArtist(ctx, name, primaryCatalog, riverJobID)
	case "deezer":
		counterpart, counterpartCatalog, overlap, err = s.matchAppleArtist(ctx, name, primaryCatalog, riverJobID, credentials)
	}
	if err != nil {
		slog.WarnContext(ctx, "storefront artist counterpart lookup failed", "provider", primary.ProviderRecord.Provider, "provider_artist_id", primary.ProviderRecord.Value, "error", err)
		return records, catalogs
	}
	if overlap < 2 || counterpart.ProviderRecord.Value == "" {
		return records, catalogs
	}
	identity := counterpart.IdentityCandidates[0]
	identity.Evidence = "unique_catalog_overlap"
	primary.IdentityCandidates = append(primary.IdentityCandidates, identity)
	records[0] = primary
	records = append(records, counterpart)
	catalogs = append(catalogs, counterpartCatalog...)
	slog.InfoContext(ctx, "storefront artist identities reconciled",
		"primary_provider", primary.ProviderRecord.Provider,
		"primary_artist_id", primary.ProviderRecord.Value,
		"counterpart_provider", counterpart.ProviderRecord.Provider,
		"counterpart_artist_id", counterpart.ProviderRecord.Value,
		"release_overlap", overlap,
	)
	return records, catalogs
}

func (s *Service) matchDeezerArtist(ctx context.Context, name string, anchor []musiccatalog.IdentityRelease, riverJobID int64) (artistdomain.NormalizedRecordV1, []musiccatalog.IdentityRelease, int, error) {
	base := deezer.New(s.runtime.Config.Providers.Deezer)
	resolver, err := providercache.New(s.runtime, artistdomain.DeezerNormalizerVersion, base.Capability().RawRetention, base.Capability().ResponseCache, riverJobID)
	if err != nil {
		return artistdomain.NormalizedRecordV1{}, nil, 0, err
	}
	client := deezer.NewCached(s.runtime.Config.Providers.Deezer, resolver)
	search, err := client.Search(ctx, "artist", name, 25, 0)
	if err != nil {
		return artistdomain.NormalizedRecordV1{}, nil, 0, err
	}
	if search.StatusCode != http.StatusOK {
		return artistdomain.NormalizedRecordV1{}, nil, 0, &providers.StatusError{Provider: "deezer", StatusCode: search.StatusCode}
	}
	var envelope struct {
		Data []struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		} `json:"data"`
	}
	if err := json.Unmarshal(search.Body, &envelope); err != nil {
		return artistdomain.NormalizedRecordV1{}, nil, 0, err
	}
	type match struct {
		id      string
		catalog []musiccatalog.IdentityRelease
		overlap int
	}
	matches := []match{}
	for _, hit := range envelope.Data {
		if hit.ID < 1 || !sameArtistName(hit.Name, name) {
			continue
		}
		id := fmt.Sprint(hit.ID)
		catalog, err := collectDeezerIdentityCatalog(ctx, client, id)
		if err != nil {
			return artistdomain.NormalizedRecordV1{}, nil, 0, err
		}
		matches = append(matches, match{id: id, catalog: catalog, overlap: musiccatalog.IdentityCatalogOverlap(anchor, catalog)})
	}
	best, ok := uniqueStorefrontMatch(matches, func(value match) int { return value.overlap })
	if !ok || best.overlap < 2 {
		return artistdomain.NormalizedRecordV1{}, nil, 0, nil
	}
	payloads, err := client.Collect(ctx, providers.Identifier{Provider: "deezer", Namespace: "artist", Value: best.id})
	if err != nil {
		return artistdomain.NormalizedRecordV1{}, nil, 0, err
	}
	recorded, err := s.recordPayloads(ctx, payloads, artistdomain.DeezerNormalizerVersion, base.Capability(), riverJobID)
	if err != nil || len(recorded) == 0 {
		return artistdomain.NormalizedRecordV1{}, nil, 0, err
	}
	record, err := deezer.NormalizeArtist(recorded[0].Payload.Body, recorded[0].ID, recorded[0].Payload.ObservedAt)
	return record, best.catalog, best.overlap, err
}

func (s *Service) matchAppleArtist(ctx context.Context, name string, anchor []musiccatalog.IdentityRelease, riverJobID int64, credentials providercredentials.Credentials) (artistdomain.NormalizedRecordV1, []musiccatalog.IdentityRelease, int, error) {
	base := apple.New(s.runtime.Config.Providers.Apple)
	resolver, err := providercache.New(s.runtime, artistdomain.AppleNormalizerVersion, base.Capability().RawRetention, base.Capability().ResponseCache, riverJobID)
	if err != nil {
		return artistdomain.NormalizedRecordV1{}, nil, 0, err
	}
	client := apple.NewCached(s.runtime.Config.Providers.Apple, resolver, credentials.APIKey("apple"))
	search, err := client.Search(ctx, "artist", name, "", 25)
	if err != nil {
		return artistdomain.NormalizedRecordV1{}, nil, 0, err
	}
	if search.StatusCode != http.StatusOK {
		return artistdomain.NormalizedRecordV1{}, nil, 0, &providers.StatusError{Provider: "apple", StatusCode: search.StatusCode}
	}
	var envelope struct {
		Results []struct {
			WrapperType string `json:"wrapperType"`
			ArtistID    int64  `json:"artistId"`
			ArtistName  string `json:"artistName"`
		} `json:"results"`
	}
	if err := json.Unmarshal(search.Body, &envelope); err != nil {
		return artistdomain.NormalizedRecordV1{}, nil, 0, err
	}
	type match struct {
		id       string
		catalog  []musiccatalog.IdentityRelease
		overlap  int
		payloads []providers.Payload
	}
	matches := []match{}
	seen := map[string]bool{}
	for _, hit := range envelope.Results {
		id := fmt.Sprint(hit.ArtistID)
		if !strings.EqualFold(hit.WrapperType, "artist") || hit.ArtistID < 1 || seen[id] || !sameArtistName(hit.ArtistName, name) {
			continue
		}
		seen[id] = true
		payloads, err := client.Collect(ctx, providers.Identifier{Provider: "apple", Namespace: "artist", Value: id})
		if err != nil || len(payloads) == 0 {
			continue
		}
		catalog, err := musiccatalog.AppleIdentityCatalog(payloads[0].Body, id)
		if err != nil {
			return artistdomain.NormalizedRecordV1{}, nil, 0, err
		}
		matches = append(matches, match{id: id, catalog: catalog, overlap: musiccatalog.IdentityCatalogOverlap(anchor, catalog), payloads: payloads})
	}
	best, ok := uniqueStorefrontMatch(matches, func(value match) int { return value.overlap })
	if !ok || best.overlap < 2 {
		return artistdomain.NormalizedRecordV1{}, nil, 0, nil
	}
	recorded, err := s.recordPayloads(ctx, best.payloads, artistdomain.AppleNormalizerVersion, base.Capability(), riverJobID)
	if err != nil || len(recorded) == 0 {
		return artistdomain.NormalizedRecordV1{}, nil, 0, err
	}
	record, err := apple.NormalizeArtist(recorded[0].Payload.Body, best.id, recorded[0].ID, recorded[0].Payload.ObservedAt)
	return record, best.catalog, best.overlap, err
}

func uniqueStorefrontMatch[T any](values []T, score func(T) int) (T, bool) {
	var zero T
	bestIndex, bestScore, tied := -1, 0, false
	for index, value := range values {
		valueScore := score(value)
		if valueScore > bestScore {
			bestIndex, bestScore, tied = index, valueScore, false
		} else if valueScore == bestScore && valueScore > 0 {
			tied = true
		}
	}
	if bestIndex < 0 || tied {
		return zero, false
	}
	return values[bestIndex], true
}

func sameArtistName(left, right string) bool {
	return artistSlug(left) == artistSlug(right)
}

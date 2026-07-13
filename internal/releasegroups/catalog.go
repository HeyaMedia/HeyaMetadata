package releasegroups

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/changelog"
	rgdomain "github.com/HeyaMedia/HeyaMetadata/internal/domains/releasegroup"
)

// CatalogSource is already identity-gated evidence from an artist-scoped
// catalog. It is deliberately smaller than any provider response.
type CatalogSource struct {
	Provider, Namespace, ID string
	Title, Date, Kind, URL  string
	ArtistName              string
	ObservationID           string
	ObservedAt              time.Time
	TrackCount              int
}

// PromoteCatalogCluster creates or updates a canonical release group that has
// no MusicBrainz spine. Callers must require independent provider agreement or
// a single storefront catalog whose artist identity and page shape were gated.
func (s *Service) PromoteCatalogCluster(ctx context.Context, artistEntityID string, sources []CatalogSource) (Result, error) {
	records := make([]rgdomain.NormalizedRecordV1, 0, len(sources))
	for _, source := range sources {
		if source.Provider == "lastfm" || source.Provider == "musicbrainz" || source.Provider == "" || source.Namespace == "" || source.ID == "" || source.Title == "" || source.ObservationID == "" {
			continue
		}
		observedAt := source.ObservedAt
		if observedAt.IsZero() {
			observedAt = time.Now().UTC()
		}
		date := rgdomain.DateValue{Value: source.Date, Precision: catalogDatePrecision(source.Date), Type: "release"}
		record := rgdomain.NormalizedRecordV1{
			ProviderRecord: rgdomain.ProviderRecord{
				Provider: source.Provider, Namespace: source.Namespace, Value: source.ID,
				PrimaryObservationID: source.ObservationID, ObservedAt: observedAt,
				NormalizerVersion: fmt.Sprintf("catalog-cluster-%s-%s/v1", source.Provider, source.ID),
				SchemaVersion:     rgdomain.NormalizedSchemaVersion,
			},
			IdentityCandidates: []rgdomain.IdentityCandidate{{Provider: source.Provider, Namespace: source.Namespace, NormalizedValue: source.ID, Confidence: 1, Evidence: "independent_catalog_consensus"}},
			Titles:             []rgdomain.Title{{Value: source.Title, Type: "catalog_title", Primary: true}},
			Classification:     rgdomain.Classification{PrimaryType: source.Kind},
			Editions: []rgdomain.Edition{{
				Provider: source.Provider, Namespace: source.Namespace, ProviderID: source.ID,
				Title: source.Title, Date: date, TrackCount: source.TrackCount, Link: source.URL,
			}},
		}
		if source.Date != "" {
			record.Dates = []rgdomain.DateValue{date}
		}
		if source.URL != "" {
			record.Links = []rgdomain.Link{{Type: source.Provider, URL: source.URL}}
		}
		if source.ArtistName != "" {
			record.ArtistCredits = []rgdomain.ArtistCredit{{Name: source.ArtistName, ArtistProvider: "heya", ArtistNamespace: "artist", ArtistID: artistEntityID, ArtistName: source.ArtistName}}
		}
		records = append(records, record)
	}
	if len(records) == 0 {
		return Result{}, fmt.Errorf("provider-only release group promotion requires normalized catalog evidence")
	}
	evidence := "identity_gated_artist_catalog"
	if len(records) > 1 {
		evidence = "independent_catalog_consensus"
	}
	for i := range records {
		for j := range records[i].IdentityCandidates {
			records[i].IdentityCandidates[j].Evidence = evidence
		}
	}
	ids := make([]string, 0, len(records))
	for _, record := range records {
		id, err := s.recordNormalized(ctx, record)
		if err != nil {
			return Result{}, err
		}
		ids = append(ids, id)
	}
	result, err := s.merge(ctx, ids, records, 0)
	if err != nil {
		return Result{}, err
	}
	if err := s.cache(ctx, result); err != nil {
		return Result{}, err
	}
	if err := changelog.Sequence(ctx, s.runtime, 100); err != nil {
		return Result{}, err
	}
	return result, nil
}

func catalogDatePrecision(value string) string {
	value = strings.TrimSpace(value)
	switch {
	case len(value) >= 10:
		return "day"
	case len(value) >= 7:
		return "month"
	case len(value) >= 4:
		return "year"
	default:
		return "unknown"
	}
}

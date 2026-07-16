package artists

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/accessstats"
	"github.com/HeyaMedia/HeyaMetadata/internal/canonicalrefs"
	artistdomain "github.com/HeyaMedia/HeyaMetadata/internal/domains/artist"
	"github.com/jackc/pgx/v5"
)

var ErrNotFound = fmt.Errorf("artist not found")

type TopTrackExternalID struct {
	Provider  string `json:"provider"`
	Namespace string `json:"namespace"`
	Value     string `json:"value"`
}

type TopTrack struct {
	Rank              int                  `json:"rank"`
	Title             string               `json:"title"`
	Provider          string               `json:"provider"`
	ProviderTrackID   string               `json:"provider_track_id,omitempty"`
	RecordingEntityID string               `json:"recording_entity_id,omitempty" format:"uuid"`
	ExternalIDs       []TopTrackExternalID `json:"external_ids"`
	Playcount         int64                `json:"playcount,omitempty"`
	Listeners         int64                `json:"listeners,omitempty"`
	URL               string               `json:"url,omitempty"`
	ResolutionState   string               `json:"resolution_state" enum:"materialized,unresolved"`
}

type TopTrackSource struct {
	Provider            string    `json:"provider"`
	ItemCount           int       `json:"item_count"`
	ReportedTotal       int       `json:"reported_total"`
	Truncated           bool      `json:"truncated"`
	SourceObservationID string    `json:"source_observation_id,omitempty"`
	ObservedAt          time.Time `json:"observed_at"`
	ProjectionVersion   int64     `json:"projection_version"`
}

type TopTracksPage struct {
	ArtistID string           `json:"artist_id" format:"uuid"`
	Results  []TopTrack       `json:"results"`
	Sources  []TopTrackSource `json:"sources"`
	Total    int              `json:"total"`
	Offset   int              `json:"offset"`
	Limit    int              `json:"limit"`
}

func (s *Service) Detail(ctx context.Context, entityID string) (artistdomain.DetailDocument, bool, error) {
	key := "heya:metadata:v1:api:entity:" + entityID + ":detail"
	if cached, err := s.runtime.Redis.Get(ctx, key).Bytes(); err == nil {
		var document artistdomain.DetailDocument
		if json.Unmarshal(cached, &document) == nil && document.Kind == "artist" {
			if err := hydrateArtistRelationIDs(ctx, s.runtime.DB, &document); err != nil {
				return artistdomain.DetailDocument{}, false, err
			}
			_ = accessstats.Track(ctx, s.runtime.Redis, entityID)
			return document, time.Now().Before(document.Freshness.FreshUntil), nil
		}
	}
	var body []byte
	var freshUntil time.Time
	if err := s.runtime.DB.QueryRow(ctx, `SELECT document,fresh_until FROM api_documents WHERE entity_id=$1 AND document_kind='detail'`, entityID).Scan(&body, &freshUntil); err != nil {
		if err == pgx.ErrNoRows {
			return artistdomain.DetailDocument{}, false, ErrNotFound
		}
		return artistdomain.DetailDocument{}, false, err
	}
	var document artistdomain.DetailDocument
	if err := json.Unmarshal(body, &document); err != nil {
		return artistdomain.DetailDocument{}, false, err
	}
	if document.Kind != "artist" {
		return artistdomain.DetailDocument{}, false, ErrNotFound
	}
	if err := hydrateArtistRelationIDs(ctx, s.runtime.DB, &document); err != nil {
		return artistdomain.DetailDocument{}, false, err
	}
	if ttl := time.Until(freshUntil); ttl > 0 {
		_ = s.runtime.Redis.Set(ctx, key, body, ttl).Err()
	}
	_ = accessstats.Track(ctx, s.runtime.Redis, entityID)
	return document, time.Now().Before(freshUntil), nil
}

func hydrateArtistRelationIDs(ctx context.Context, db canonicalrefs.Querier, document *artistdomain.DetailDocument) error {
	refs := make([]canonicalrefs.Ref, 0, len(document.Data.Relationships)+len(document.Data.SimilarArtists))
	for _, relationship := range document.Data.Relationships {
		refs = append(refs, canonicalrefs.Ref{Provider: relationship.TargetProvider, Namespace: relationship.TargetNamespace, Value: relationship.TargetID})
	}
	for _, similar := range document.Data.SimilarArtists {
		refs = append(refs, canonicalrefs.Ref{Provider: similar.Provider, Namespace: "artist", Value: similar.ProviderID})
	}
	resolved, err := canonicalrefs.Resolve(ctx, db, "artist", refs)
	if err != nil {
		return err
	}
	for index := range document.Data.Relationships {
		relationship := &document.Data.Relationships[index]
		ref := canonicalrefs.Ref{Provider: relationship.TargetProvider, Namespace: relationship.TargetNamespace, Value: relationship.TargetID}
		relationship.EntityID = resolved[canonicalrefs.Key(ref)]
		relationship.ResolutionState = "unresolved"
		if relationship.EntityID != "" {
			relationship.ResolutionState = "materialized"
		}
	}
	for index := range document.Data.SimilarArtists {
		similar := &document.Data.SimilarArtists[index]
		ref := canonicalrefs.Ref{Provider: similar.Provider, Namespace: "artist", Value: similar.ProviderID}
		similar.EntityID = resolved[canonicalrefs.Key(ref)]
		similar.ResolutionState = "unresolved"
		if similar.EntityID != "" {
			similar.ResolutionState = "materialized"
		}
	}
	return nil
}

func (s *Service) TopTracks(ctx context.Context, entityID string, offset, limit int) (TopTracksPage, error) {
	if offset < 0 {
		offset = 0
	}
	if limit < 1 || limit > 100 {
		limit = 50
	}
	page := TopTracksPage{ArtistID: entityID, Results: []TopTrack{}, Sources: []TopTrackSource{}, Offset: offset, Limit: limit}
	var exists bool
	if err := s.runtime.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM entities WHERE id=$1 AND kind='artist' AND deleted_at IS NULL)`, entityID).Scan(&exists); err != nil {
		return TopTracksPage{}, err
	}
	if !exists {
		return TopTracksPage{}, ErrNotFound
	}
	if err := s.runtime.DB.QueryRow(ctx, `SELECT count(*) FROM artist_top_tracks WHERE artist_entity_id=$1`, entityID).Scan(&page.Total); err != nil {
		return TopTracksPage{}, err
	}
	sourceRows, err := s.runtime.DB.Query(ctx, `SELECT provider,item_count,reported_total,COALESCE(source_observation_id::text,''),observed_at,projection_version FROM artist_top_track_snapshots WHERE artist_entity_id=$1 ORDER BY CASE provider WHEN 'lastfm' THEN 0 ELSE 100 END,provider`, entityID)
	if err != nil {
		return TopTracksPage{}, err
	}
	for sourceRows.Next() {
		var source TopTrackSource
		if err := sourceRows.Scan(&source.Provider, &source.ItemCount, &source.ReportedTotal, &source.SourceObservationID, &source.ObservedAt, &source.ProjectionVersion); err != nil {
			sourceRows.Close()
			return TopTracksPage{}, err
		}
		source.Truncated = source.ReportedTotal > source.ItemCount
		page.Sources = append(page.Sources, source)
	}
	if err := sourceRows.Err(); err != nil {
		sourceRows.Close()
		return TopTracksPage{}, err
	}
	sourceRows.Close()
	rows, err := s.runtime.DB.Query(ctx, `SELECT track.rank,track.title,track.provider,track.provider_track_id,track.recording_mbid,track.playcount,track.listeners,track.url,COALESCE((SELECT claim.entity_id::text FROM external_id_claims claim JOIN entities recording ON recording.id=claim.entity_id AND recording.deleted_at IS NULL WHERE claim.entity_kind='recording'AND claim.provider='musicbrainz'AND claim.namespace='recording'AND claim.normalized_value=track.recording_mbid AND claim.state='accepted' LIMIT 1),'') FROM artist_top_tracks track WHERE track.artist_entity_id=$1 ORDER BY CASE track.provider WHEN 'lastfm' THEN 0 ELSE 100 END,track.provider,track.rank,lower(track.title) OFFSET $2 LIMIT $3`, entityID, offset, limit)
	if err != nil {
		return TopTracksPage{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var track TopTrack
		var recordingMBID string
		if err := rows.Scan(&track.Rank, &track.Title, &track.Provider, &track.ProviderTrackID, &recordingMBID, &track.Playcount, &track.Listeners, &track.URL, &track.RecordingEntityID); err != nil {
			return TopTracksPage{}, err
		}
		track.ExternalIDs = []TopTrackExternalID{}
		if recordingMBID != "" {
			track.ExternalIDs = append(track.ExternalIDs, TopTrackExternalID{Provider: "musicbrainz", Namespace: "recording", Value: recordingMBID})
		}
		track.ResolutionState = "unresolved"
		if track.RecordingEntityID != "" {
			track.ResolutionState = "materialized"
		}
		page.Results = append(page.Results, track)
	}
	if err := rows.Err(); err != nil {
		return TopTracksPage{}, err
	}
	_ = accessstats.Track(ctx, s.runtime.Redis, entityID)
	return page, nil
}
func (s *Service) Resolve(ctx context.Context, provider, namespace, value string) (string, error) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	namespace = strings.ToLower(strings.TrimSpace(namespace))
	value = strings.TrimSpace(value)
	if provider == "wikidata" {
		value = strings.ToUpper(value)
	}
	if provider == "musicbrainz" {
		value = strings.ToLower(value)
	}
	var id string
	err := s.runtime.DB.QueryRow(ctx, `SELECT entity_id FROM external_id_claims WHERE entity_kind='artist' AND provider=$1 AND namespace=$2 AND normalized_value=$3 AND state='accepted'`, provider, namespace, value).Scan(&id)
	if err == pgx.ErrNoRows {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("resolve artist external ID: %w", err)
	}
	return id, nil
}

// CanonicalID follows explicit entity redirects and returns the active artist
// UUID. This keeps operator tooling on canonical Heya identities even after an
// artist consolidation retires the originally supplied UUID.
func (s *Service) CanonicalID(ctx context.Context, entityID string) (string, error) {
	var canonicalID string
	err := s.runtime.DB.QueryRow(ctx, `WITH RECURSIVE chain(id,depth)AS(SELECT $1::uuid,0 UNION ALL SELECT redirect.survivor_entity_id,chain.depth+1 FROM chain JOIN entity_redirects redirect ON redirect.retired_entity_id=chain.id WHERE chain.depth<16)SELECT chain.id::text FROM chain JOIN entities entity ON entity.id=chain.id WHERE entity.kind='artist' AND entity.deleted_at IS NULL ORDER BY chain.depth DESC LIMIT 1`, entityID).Scan(&canonicalID)
	if err == pgx.ErrNoRows {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("resolve canonical artist ID: %w", err)
	}
	return canonicalID, nil
}

func (s *Service) MusicBrainzID(ctx context.Context, entityID string) (string, error) {
	var value string
	err := s.runtime.DB.QueryRow(ctx, `SELECT normalized_value FROM external_id_claims WHERE entity_id=$1 AND entity_kind='artist' AND provider='musicbrainz' AND namespace='artist' AND state='accepted'`, entityID).Scan(&value)
	if err == pgx.ErrNoRows {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	return value, nil
}

// RefreshRoot returns the best directly ingestible artist identity. Provider
// selection is private to HeyaMetadata and does not leak into client control
// flow.
func (s *Service) RefreshRoot(ctx context.Context, entityID string) (provider, value string, err error) {
	err = s.runtime.DB.QueryRow(ctx, `
		SELECT claim.provider,claim.normalized_value
		FROM external_id_claims claim
		WHERE claim.entity_id=$1 AND claim.entity_kind='artist' AND claim.namespace='artist' AND claim.state='accepted'
		  AND claim.provider IN('musicbrainz','apple','deezer')
		  AND (claim.provider='musicbrainz' OR EXISTS(
			SELECT 1 FROM normalized_records record
			WHERE record.entity_id=claim.entity_id AND record.entity_kind='artist'
			  AND record.provider=claim.provider AND record.provider_namespace='artist'
			  AND record.provider_record_id=claim.normalized_value))
		ORDER BY CASE claim.provider WHEN 'musicbrainz' THEN 1 WHEN 'apple' THEN 2 ELSE 3 END,
		         claim.last_observed_at DESC
		LIMIT 1`, entityID).Scan(&provider, &value)
	if err == pgx.ErrNoRows {
		return "", "", ErrNotFound
	}
	return provider, value, err
}

package artists

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/accessstats"
	artistdomain "github.com/HeyaMedia/HeyaMetadata/internal/domains/artist"
	"github.com/jackc/pgx/v5"
)

var ErrNotFound = fmt.Errorf("artist not found")

func (s *Service) Detail(ctx context.Context, entityID string) (artistdomain.DetailDocument, bool, error) {
	key := "heya:metadata:v1:api:entity:" + entityID + ":detail"
	if cached, err := s.runtime.Redis.Get(ctx, key).Bytes(); err == nil {
		var document artistdomain.DetailDocument
		if json.Unmarshal(cached, &document) == nil && document.Kind == "artist" {
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
	if ttl := time.Until(freshUntil); ttl > 0 {
		_ = s.runtime.Redis.Set(ctx, key, body, ttl).Err()
	}
	_ = accessstats.Track(ctx, s.runtime.Redis, entityID)
	return document, time.Now().Before(freshUntil), nil
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

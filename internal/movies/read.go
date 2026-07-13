package movies

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/accessstats"
	moviedomain "github.com/HeyaMedia/HeyaMetadata/internal/domains/movie"
	"github.com/jackc/pgx/v5"
)

func (s *Service) Detail(ctx context.Context, entityID string) (moviedomain.DetailDocument, bool, error) {
	cacheKey := "heya:metadata:v1:api:entity:" + entityID + ":detail"
	if cached, err := s.runtime.Redis.Get(ctx, cacheKey).Bytes(); err == nil {
		var document moviedomain.DetailDocument
		if json.Unmarshal(cached, &document) == nil {
			if err := hydrateRecommendationIDs(ctx, s.runtime.DB, &document); err != nil {
				return moviedomain.DetailDocument{}, false, err
			}
			if err := hydrateCreditPersonIDs(ctx, s.runtime.DB, &document); err != nil {
				return moviedomain.DetailDocument{}, false, err
			}
			_ = accessstats.Track(ctx, s.runtime.Redis, entityID)
			return document, time.Now().Before(document.Freshness.FreshUntil), nil
		}
	}
	var body []byte
	var freshUntil time.Time
	if err := s.runtime.DB.QueryRow(ctx, `
        SELECT document, fresh_until FROM api_documents
        WHERE entity_id = $1 AND document_kind = 'detail'`, entityID).Scan(&body, &freshUntil); err != nil {
		if err == pgx.ErrNoRows {
			return moviedomain.DetailDocument{}, false, ErrNotFound
		}
		return moviedomain.DetailDocument{}, false, fmt.Errorf("read movie detail: %w", err)
	}
	var document moviedomain.DetailDocument
	if err := json.Unmarshal(body, &document); err != nil {
		return moviedomain.DetailDocument{}, false, err
	}
	if err := hydrateRecommendationIDs(ctx, s.runtime.DB, &document); err != nil {
		return moviedomain.DetailDocument{}, false, err
	}
	if err := hydrateCreditPersonIDs(ctx, s.runtime.DB, &document); err != nil {
		return moviedomain.DetailDocument{}, false, err
	}
	ttl := time.Until(freshUntil)
	if ttl > 0 {
		_ = s.runtime.Redis.Set(ctx, cacheKey, body, ttl).Err()
	}
	_ = accessstats.Track(ctx, s.runtime.Redis, entityID)
	return document, time.Now().Before(freshUntil), nil
}

type recommendationQuerier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func hydrateRecommendationIDs(ctx context.Context, db recommendationQuerier, document *moviedomain.DetailDocument) error {
	if len(document.Data.Recommendations) == 0 {
		return nil
	}
	providers := make([]string, 0, len(document.Data.Recommendations))
	values := make([]string, 0, len(document.Data.Recommendations))
	for i := range document.Data.Recommendations {
		provider := document.Data.Recommendations[i].Provider
		if provider == "" {
			provider = "tmdb"
			document.Data.Recommendations[i].Provider = provider
		}
		providers = append(providers, provider)
		values = append(values, document.Data.Recommendations[i].ProviderTargetID)
	}
	rows, err := db.Query(ctx, `SELECT provider,normalized_value,entity_id::text FROM external_id_claims WHERE entity_kind='movie' AND namespace='movie' AND state='accepted' AND provider=ANY($1) AND normalized_value=ANY($2)`, providers, values)
	if err != nil {
		return err
	}
	defer rows.Close()
	resolved := map[string]string{}
	for rows.Next() {
		var provider, value, entityID string
		if err := rows.Scan(&provider, &value, &entityID); err != nil {
			return err
		}
		resolved[provider+":"+value] = entityID
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for i := range document.Data.Recommendations {
		recommendation := &document.Data.Recommendations[i]
		recommendation.EntityID = resolved[recommendation.Provider+":"+recommendation.ProviderTargetID]
	}
	return nil
}

func hydrateCreditPersonIDs(ctx context.Context, db recommendationQuerier, document *moviedomain.DetailDocument) error {
	if len(document.Data.Credits) == 0 {
		return nil
	}
	providers := make([]string, 0, len(document.Data.Credits))
	values := make([]string, 0, len(document.Data.Credits))
	for _, credit := range document.Data.Credits {
		providers = append(providers, credit.Provider)
		values = append(values, credit.ProviderPersonID)
	}
	rows, err := db.Query(ctx, `SELECT provider,normalized_value,entity_id::text FROM external_id_claims WHERE entity_kind='person' AND namespace='person' AND state='accepted' AND provider=ANY($1) AND normalized_value=ANY($2)`, providers, values)
	if err != nil {
		return err
	}
	defer rows.Close()
	resolved := map[string]string{}
	for rows.Next() {
		var provider, value, entityID string
		if err := rows.Scan(&provider, &value, &entityID); err != nil {
			return err
		}
		resolved[provider+":"+value] = entityID
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for i := range document.Data.Credits {
		credit := &document.Data.Credits[i]
		credit.PersonEntityID = resolved[credit.Provider+":"+credit.ProviderPersonID]
	}
	return nil
}

func (s *Service) Resolve(ctx context.Context, provider, namespace, value string) (string, error) {
	var entityID string
	err := s.runtime.DB.QueryRow(ctx, `
        SELECT entity_id FROM external_id_claims
        WHERE entity_kind = 'movie' AND provider = $1 AND namespace = $2
          AND normalized_value = $3 AND state = 'accepted'`,
		strings.ToLower(provider), strings.ToLower(namespace), normalizeExternalValue(provider, namespace, value),
	).Scan(&entityID)
	if err == pgx.ErrNoRows {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("resolve movie external ID: %w", err)
	}
	return entityID, nil
}

func (s *Service) TMDBID(ctx context.Context, entityID string) (int64, error) {
	var value int64
	err := s.runtime.DB.QueryRow(ctx, `
        SELECT normalized_value::bigint FROM external_id_claims
        WHERE entity_id = $1 AND entity_kind = 'movie' AND provider = 'tmdb'
          AND namespace = 'movie' AND state = 'accepted'`, entityID).Scan(&value)
	if err == pgx.ErrNoRows {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("read TMDB claim: %w", err)
	}
	return value, nil
}

type SearchFilters struct {
	Year                             int
	Genre, Country, Language, Status string
}

func (s *Service) Search(ctx context.Context, query string, filters SearchFilters, limit int) ([]moviedomain.SummaryDocument, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	digest := sha256.Sum256([]byte(strings.ToLower(query) + fmt.Sprintf(":%d:%+v", limit, filters)))
	cacheKey := "heya:metadata:v1:search:" + hex.EncodeToString(digest[:])
	if cached, err := s.runtime.Redis.Get(ctx, cacheKey).Bytes(); err == nil {
		var documents []moviedomain.SummaryDocument
		if json.Unmarshal(cached, &documents) == nil {
			return documents, nil
		}
	}
	var documents []moviedomain.SummaryDocument
	seen := map[string]bool{}
	externalProvider := ""
	externalValue := query
	if parts := strings.SplitN(query, ":", 2); len(parts) == 2 {
		externalProvider, externalValue = strings.ToLower(parts[0]), parts[1]
	}
	externalRows, err := s.runtime.DB.Query(ctx, `
        SELECT se.summary
        FROM external_id_claims claims
        JOIN search_entities se ON se.entity_id = claims.entity_id
        WHERE claims.entity_kind = 'movie' AND claims.state = 'accepted'
          AND lower(claims.normalized_value) = lower($1)
          AND ($2 = '' OR claims.provider = $2)
          AND ($4 = 0 OR se.release_year = $4)
          AND ($5 = '' OR EXISTS (SELECT 1 FROM unnest(se.genres) value WHERE lower(value)=lower($5)))
          AND ($6 = '' OR upper($6) = ANY(se.countries))
          AND ($7 = '' OR lower($7) = ANY(se.languages))
          AND ($8 = '' OR se.status = lower($8))
        ORDER BY se.popularity DESC NULLS LAST
        LIMIT $3`, externalValue, externalProvider, limit, filters.Year, filters.Genre, filters.Country, filters.Language, filters.Status)
	if err != nil {
		return nil, fmt.Errorf("search movie external IDs: %w", err)
	}
	for externalRows.Next() {
		var body []byte
		if err := externalRows.Scan(&body); err != nil {
			externalRows.Close()
			return nil, err
		}
		var document moviedomain.SummaryDocument
		if err := json.Unmarshal(body, &document); err != nil {
			externalRows.Close()
			return nil, err
		}
		if !seen[document.ID] {
			seen[document.ID] = true
			documents = append(documents, document)
		}
	}
	externalRows.Close()
	if len(documents) >= limit {
		if body, err := json.Marshal(documents); err == nil {
			_ = s.runtime.Redis.Set(ctx, cacheKey, body, 5*time.Minute).Err()
		}
		return documents, nil
	}
	rows, err := s.runtime.DB.Query(ctx, `
        SELECT se.summary
        FROM search_entities se
        JOIN LATERAL (
            SELECT min(CASE
                WHEN sn.normalized_value = lower(unaccent($1)) THEN 0
                WHEN sn.normalized_value LIKE lower(unaccent($1)) || '%' THEN 1
                ELSE 2 END) AS tier,
                max(similarity(sn.normalized_value, lower(unaccent($1)))) AS score
            FROM search_names sn
            WHERE sn.entity_id = se.entity_id AND (
                sn.normalized_value = lower(unaccent($1)) OR
                sn.normalized_value LIKE lower(unaccent($1)) || '%' OR
                similarity(sn.normalized_value, lower(unaccent($1))) >= 0.25
            )
        ) matches ON matches.tier IS NOT NULL
        WHERE se.kind = 'movie'
          AND ($3 = 0 OR se.release_year = $3)
          AND ($4 = '' OR EXISTS (SELECT 1 FROM unnest(se.genres) value WHERE lower(value)=lower($4)))
          AND ($5 = '' OR upper($5) = ANY(se.countries))
          AND ($6 = '' OR lower($6) = ANY(se.languages))
          AND ($7 = '' OR se.status = lower($7))
        ORDER BY matches.tier, matches.score DESC, se.popularity DESC NULLS LAST, se.display_title
		LIMIT $2`, query, limit-len(documents), filters.Year, filters.Genre, filters.Country, filters.Language, filters.Status)
	if err != nil {
		return nil, fmt.Errorf("search movies: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var body []byte
		if err := rows.Scan(&body); err != nil {
			return nil, err
		}
		var document moviedomain.SummaryDocument
		if err := json.Unmarshal(body, &document); err != nil {
			return nil, err
		}
		if !seen[document.ID] {
			seen[document.ID] = true
			documents = append(documents, document)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if body, err := json.Marshal(documents); err == nil {
		_ = s.runtime.Redis.Set(ctx, cacheKey, body, 5*time.Minute).Err()
	}
	return documents, nil
}

func normalizeExternalValue(provider, namespace, value string) string {
	value = strings.TrimSpace(value)
	if strings.EqualFold(provider, "imdb") && strings.EqualFold(namespace, "title") {
		return strings.ToLower(value)
	}
	if strings.EqualFold(provider, "wikidata") {
		return strings.ToUpper(value)
	}
	if strings.EqualFold(provider, "tmdb") {
		if number, err := strconv.ParseInt(value, 10, 64); err == nil && number > 0 {
			return strconv.FormatInt(number, 10)
		}
	}
	return value
}

var ErrNotFound = fmt.Errorf("movie not found")

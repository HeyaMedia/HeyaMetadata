package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/images"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/HeyaMedia/HeyaMetadata/internal/resourceid"
	"github.com/danielgtaylor/huma/v2"
)

const (
	libraryStatsCacheKey         = "heya:metadata:v2:library-stats"
	libraryStatsRefreshLockKey   = libraryStatsCacheKey + ":refresh"
	libraryStatsFreshFor         = 5 * time.Minute
	libraryStatsCacheRetention   = 24 * time.Hour
	movieCollectionsCacheKey     = "heya:metadata:v2:movie-collections"
	movieCollectionsCacheRefresh = time.Hour
)

type browseInput struct {
	Query          string `query:"q" maxLength:"200" doc:"Optional local title/name query"`
	Kind           string `query:"kind" maxLength:"50" doc:"Optional exact canonical kind"`
	Sort           string `query:"sort" enum:"updated,title,year,popular" default:"updated"`
	Offset         int    `query:"offset" minimum:"0" default:"0"`
	Limit          int    `query:"limit" minimum:"1" maximum:"100" default:"24"`
	AcceptLanguage string `header:"Accept-Language" doc:"Preferred presentation languages for compact titles and names"`
	primaryOnly    bool
	skipTotal      bool
}
type browseOutput struct {
	Vary string `header:"Vary"`
	Body struct {
		Results []json.RawMessage `json:"results"`
		Total   int64             `json:"total"`
		Offset  int               `json:"offset"`
		Limit   int               `json:"limit"`
	}
}
type latestInput struct {
	Kind           string `query:"kind" maxLength:"50" doc:"Optional exact canonical kind"`
	Limit          int    `query:"limit" minimum:"1" maximum:"100" default:"24"`
	AcceptLanguage string `header:"Accept-Language" doc:"Preferred presentation languages for compact titles and names"`
}
type statsOutput struct {
	Body struct {
		Entities        int64            `json:"entities"`
		Kinds           map[string]int64 `json:"kinds"`
		ProviderClaims  map[string]int64 `json:"provider_claims"`
		Images          int64            `json:"images"`
		Materialized    int64            `json:"materialized_images"`
		ProviderRecords int64            `json:"provider_records"`
		Fresh           int64            `json:"fresh"`
		Stale           int64            `json:"stale"`
		GeneratedAt     time.Time        `json:"generated_at"`
	}
}
type collectionMember struct {
	ProviderID      string `json:"provider_id"`
	EntityID        string `json:"entity_id,omitempty" format:"uuid"`
	Title           string `json:"title"`
	Year            int    `json:"year,omitempty"`
	ImageID         string `json:"image_id,omitempty"`
	Order           int    `json:"order"`
	ResolutionState string `json:"resolution_state" enum:"materialized,unresolved"`
}
type collectionCard struct {
	ID         string             `json:"id" format:"uuid"`
	Provider   string             `json:"provider"`
	ProviderID string             `json:"provider_id"`
	Name       string             `json:"name"`
	Overview   string             `json:"overview,omitempty"`
	ImageID    string             `json:"image_id,omitempty"`
	Members    []collectionMember `json:"members"`
}
type collectionsOutput struct {
	Body struct {
		Collections []collectionCard `json:"collections"`
	}
}
type collectionInput struct {
	ID string `path:"id" format:"uuid"`
}
type collectionOutput struct{ Body collectionCard }

func registerLibrary(api huma.API, runtime *platform.Runtime) {
	huma.Register(api, huma.Operation{OperationID: "browse-library", Method: http.MethodGet, Path: "/api/v2/browse", Summary: "Browse canonical entities", Tags: []string{"Library"}}, func(ctx context.Context, input *browseInput) (*browseOutput, error) {
		if runtime == nil {
			return nil, huma.Error503ServiceUnavailable("runtime is unavailable")
		}
		return browseLibrary(ctx, runtime, input)
	})
	huma.Register(api, huma.Operation{OperationID: "latest-library", Method: http.MethodGet, Path: "/api/v2/latest", Summary: "Recently updated canonical entities", Tags: []string{"Library"}}, func(ctx context.Context, input *latestInput) (*browseOutput, error) {
		if runtime == nil {
			return nil, huma.Error503ServiceUnavailable("runtime is unavailable")
		}
		return browseLibrary(ctx, runtime, &browseInput{Kind: input.Kind, Sort: "updated", Limit: input.Limit, AcceptLanguage: input.AcceptLanguage, primaryOnly: input.Kind == "", skipTotal: true})
	})
	huma.Register(api, huma.Operation{OperationID: "library-stats", Method: http.MethodGet, Path: "/api/v2/stats", Summary: "Canonical library coverage statistics", Tags: []string{"Library"}}, func(ctx context.Context, _ *struct{}) (*statsOutput, error) {
		if runtime == nil {
			return nil, huma.Error503ServiceUnavailable("runtime is unavailable")
		}
		return libraryStats(ctx, runtime)
	})
	huma.Register(api, huma.Operation{OperationID: "collections-list", Method: http.MethodGet, Path: "/api/v2/collections", Summary: "Movie franchises encountered in canonical metadata", Tags: []string{"Library", "Collections"}}, func(ctx context.Context, _ *struct{}) (*collectionsOutput, error) {
		if runtime == nil {
			return nil, huma.Error503ServiceUnavailable("runtime is unavailable")
		}
		values, err := movieCollections(ctx, runtime)
		if err != nil {
			return nil, err
		}
		out := &collectionsOutput{}
		out.Body.Collections = values
		return out, nil
	})
	huma.Register(api, huma.Operation{OperationID: "collection-detail", Method: http.MethodGet, Path: "/api/v2/collections/{id}", Summary: "One movie franchise and its known members", Tags: []string{"Library", "Collections"}}, func(ctx context.Context, input *collectionInput) (*collectionOutput, error) {
		if runtime == nil {
			return nil, huma.Error503ServiceUnavailable("runtime is unavailable")
		}
		values, err := movieCollections(ctx, runtime)
		if err != nil {
			return nil, err
		}
		for _, value := range values {
			if value.ID == input.ID {
				return &collectionOutput{Body: value}, nil
			}
		}
		return nil, huma.Error404NotFound("collection not found")
	})
}

func browseLibrary(ctx context.Context, runtime *platform.Runtime, input *browseInput) (*browseOutput, error) {
	limit := input.Limit
	if limit < 1 || limit > 100 {
		limit = 24
	}
	offset := input.Offset
	if offset < 0 {
		offset = 0
	}
	order := "se.updated_at DESC,se.display_title"
	switch input.Sort {
	case "title":
		order = "se.display_title,se.updated_at DESC"
	case "year":
		order = "se.release_year DESC NULLS LAST,se.display_title"
	case "popular":
		order = "se.popularity DESC NULLS LAST,se.display_title"
	}
	query := strings.TrimSpace(input.Query)
	kind := strings.ToLower(strings.TrimSpace(input.Kind))
	where := `($1='' OR se.kind=$1) AND ($2='' OR EXISTS(SELECT 1 FROM search_names sn WHERE sn.entity_id=se.entity_id AND (sn.normalized_value LIKE lower(unaccent($2))||'%' OR similarity(sn.normalized_value,lower(unaccent($2)))>=0.25)))`
	if input.primaryOnly {
		where += ` AND se.kind NOT IN ('person','author')`
	}
	out := &browseOutput{}
	out.Vary = "Accept-Language"
	out.Body.Offset = offset
	out.Body.Limit = limit
	out.Body.Results = []json.RawMessage{}
	// The exact count walks every index entry for the kind — a cold-cache table
	// scan that took double-digit seconds for release_group in production.
	// latest is a shelf, not a paginator, so it never displays the total.
	if !input.skipTotal {
		if err := runtime.DB.QueryRow(ctx, `SELECT count(*) FROM search_entities se WHERE `+where, kind, query).Scan(&out.Body.Total); err != nil {
			return nil, err
		}
	}
	rows, err := runtime.DB.Query(ctx, `SELECT se.summary FROM search_entities se WHERE `+where+` ORDER BY `+order+` OFFSET $3 LIMIT $4`, kind, query, offset, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var body []byte
		if err := rows.Scan(&body); err != nil {
			return nil, err
		}
		out.Body.Results = append(out.Body.Results, json.RawMessage(body))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if input.skipTotal {
		out.Body.Total = int64(len(out.Body.Results))
	}
	preferences := images.LanguagePreferences("", "", input.AcceptLanguage)
	out.Body.Results, err = localizeSummaries(ctx, runtime, out.Body.Results, preferences)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func libraryStats(ctx context.Context, runtime *platform.Runtime) (*statsOutput, error) {
	out := &statsOutput{}
	if cached, err := runtime.Redis.Get(ctx, libraryStatsCacheKey).Bytes(); err == nil && json.Unmarshal(cached, &out.Body) == nil {
		if time.Since(out.Body.GeneratedAt) > libraryStatsFreshFor {
			refreshLibraryStatsAsync(runtime)
		}
		return out, nil
	}
	return computeLibraryStats(ctx, runtime)
}

func computeLibraryStats(ctx context.Context, runtime *platform.Runtime) (*statsOutput, error) {
	out := &statsOutput{}
	out.Body.Kinds = map[string]int64{}
	out.Body.ProviderClaims = map[string]int64{}
	out.Body.GeneratedAt = time.Now().UTC()
	rows, err := runtime.DB.Query(ctx, `SELECT kind,count(*) FROM entities WHERE deleted_at IS NULL GROUP BY kind ORDER BY kind`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var name string
		var count int64
		if err = rows.Scan(&name, &count); err != nil {
			rows.Close()
			return nil, err
		}
		out.Body.Kinds[name] = count
		out.Body.Entities += count
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	rows, err = runtime.DB.Query(ctx, `SELECT provider,count(*) FROM external_id_claims WHERE state='accepted' GROUP BY provider ORDER BY provider`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var name string
		var count int64
		if err = rows.Scan(&name, &count); err != nil {
			rows.Close()
			return nil, err
		}
		out.Body.ProviderClaims[name] = count
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if err = runtime.DB.QueryRow(ctx, `SELECT count(*),count(*)FILTER(WHERE materialization_state='ready')FROM image_candidates`).Scan(&out.Body.Images, &out.Body.Materialized); err != nil {
		return nil, err
	}
	if err = runtime.DB.QueryRow(ctx, `SELECT count(*)FROM normalized_records`).Scan(&out.Body.ProviderRecords); err != nil {
		return nil, err
	}
	if err = runtime.DB.QueryRow(ctx, `SELECT count(*)FILTER(WHERE fresh_until>now()),count(*)FILTER(WHERE fresh_until<=now())FROM api_documents WHERE document_kind='detail'`).Scan(&out.Body.Fresh, &out.Body.Stale); err != nil {
		return nil, err
	}
	if body, marshalErr := json.Marshal(out.Body); marshalErr == nil {
		if cacheErr := runtime.Redis.Set(ctx, libraryStatsCacheKey, body, libraryStatsCacheRetention).Err(); cacheErr != nil {
			slog.WarnContext(ctx, "cache library stats", "error", cacheErr)
		}
	}
	return out, nil
}

func refreshLibraryStatsAsync(runtime *platform.Runtime) {
	lockCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	acquired, err := runtime.Redis.SetNX(lockCtx, libraryStatsRefreshLockKey, "1", time.Minute).Result()
	cancel()
	if err != nil || !acquired {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		defer runtime.Redis.Del(context.WithoutCancel(ctx), libraryStatsRefreshLockKey)
		if _, err := computeLibraryStats(ctx, runtime); err != nil {
			slog.WarnContext(ctx, "refresh cached library stats", "error", err)
		}
	}()
}

func movieCollections(ctx context.Context, runtime *platform.Runtime) ([]collectionCard, error) {
	if cached, err := runtime.Redis.Get(ctx, movieCollectionsCacheKey).Bytes(); err == nil {
		var values []collectionCard
		if json.Unmarshal(cached, &values) == nil {
			return values, nil
		}
	}
	rows, err := runtime.DB.Query(ctx, `SELECT DISTINCT ON (document->'data'->'collection'->>'provider_id') document->'data'->'collection' FROM api_documents d JOIN entities e ON e.id=d.entity_id WHERE d.document_kind='detail' AND e.kind='movie' AND document->'data'->'collection'->>'provider_id'<>'' ORDER BY document->'data'->'collection'->>'provider_id',d.updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := []collectionCard{}
	for rows.Next() {
		var body []byte
		if err = rows.Scan(&body); err != nil {
			return nil, err
		}
		var raw struct {
			ProviderID string `json:"provider_id"`
			Name       string `json:"name"`
			Overview   string `json:"overview"`
			Images     []struct {
				ID string `json:"id"`
			} `json:"images"`
			Members []collectionMember `json:"members"`
		}
		if err = json.Unmarshal(body, &raw); err != nil {
			return nil, fmt.Errorf("decode collection: %w", err)
		}
		card := collectionCard{ID: resourceid.For("movie_collection", "tmdb:"+raw.ProviderID), Provider: "tmdb", ProviderID: raw.ProviderID, Name: raw.Name, Overview: raw.Overview, Members: raw.Members}
		if len(raw.Images) > 0 {
			card.ImageID = raw.Images[0].ID
		}
		if card.ImageID == "" && len(card.Members) > 0 {
			card.ImageID = card.Members[0].ImageID
		}
		values = append(values, card)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	providerIDs := make([]string, 0)
	for i := range values {
		for j := range values[i].Members {
			providerIDs = append(providerIDs, values[i].Members[j].ProviderID)
		}
	}
	entityIDs := make(map[string]string, len(providerIDs))
	if len(providerIDs) > 0 {
		claimRows, queryErr := runtime.DB.Query(ctx, `SELECT normalized_value,entity_id::text FROM external_id_claims WHERE entity_kind='movie'AND provider='tmdb'AND namespace='movie'AND normalized_value=ANY($1)AND state='accepted'`, providerIDs)
		if queryErr != nil {
			return nil, queryErr
		}
		for claimRows.Next() {
			var providerID, entityID string
			if scanErr := claimRows.Scan(&providerID, &entityID); scanErr != nil {
				claimRows.Close()
				return nil, scanErr
			}
			entityIDs[providerID] = entityID
		}
		claimRows.Close()
		if queryErr = claimRows.Err(); queryErr != nil {
			return nil, queryErr
		}
	}
	for i := range values {
		for j := range values[i].Members {
			values[i].Members[j].EntityID = entityIDs[values[i].Members[j].ProviderID]
			values[i].Members[j].ResolutionState = "unresolved"
			if values[i].Members[j].EntityID != "" {
				values[i].Members[j].ResolutionState = "materialized"
			}
		}
	}
	sort.Slice(values, func(i, j int) bool {
		if len(values[i].Members) != len(values[j].Members) {
			return len(values[i].Members) > len(values[j].Members)
		}
		return values[i].Name < values[j].Name
	})
	if body, marshalErr := json.Marshal(values); marshalErr == nil {
		if cacheErr := runtime.Redis.Set(ctx, movieCollectionsCacheKey, body, movieCollectionsCacheRefresh).Err(); cacheErr != nil {
			slog.WarnContext(ctx, "cache movie collections", "error", cacheErr)
		}
	}
	return values, nil
}

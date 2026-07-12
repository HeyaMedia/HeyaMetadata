package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/artists"
	"github.com/HeyaMedia/HeyaMetadata/internal/jobs"
	"github.com/HeyaMedia/HeyaMetadata/internal/movies"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/HeyaMedia/HeyaMetadata/internal/providercredentials"
	"github.com/HeyaMedia/HeyaMetadata/internal/releasegroups"
	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
)

type entityInput struct {
	ID            string `path:"id" format:"uuid"`
	TMDBAPIKey    string `header:"X-Heya-TMDB-API-Key" doc:"Optional request-scoped TMDB API key; never persisted"`
	OMDBAPIKey    string `header:"X-Heya-OMDB-API-Key" doc:"Optional request-scoped OMDb API key; never persisted"`
	TVDBAPIKey    string `header:"X-Heya-TVDB-API-Key" doc:"Optional request-scoped TVDB API key; never persisted"`
	FanartAPIKey  string `header:"X-Heya-Fanart-API-Key" doc:"Optional request-scoped Fanart.tv personal API key; never persisted"`
	AppleAPIKey   string `header:"X-Heya-Apple-API-Key" doc:"Optional request-scoped Apple Music developer token; never persisted"`
	DiscogsAPIKey string `header:"X-Heya-Discogs-API-Key" doc:"Optional request-scoped Discogs token; never persisted"`
	LastFMAPIKey  string `header:"X-Heya-LastFM-API-Key" doc:"Optional request-scoped Last.fm API key; never persisted"`
}
type entityOutput struct{ Body any }

type resolutionInput struct {
	Prefer        string `header:"Prefer"`
	TMDBAPIKey    string `header:"X-Heya-TMDB-API-Key" doc:"Optional request-scoped TMDB API key; never persisted"`
	OMDBAPIKey    string `header:"X-Heya-OMDB-API-Key" doc:"Optional request-scoped OMDb API key; never persisted"`
	TVDBAPIKey    string `header:"X-Heya-TVDB-API-Key" doc:"Optional request-scoped TVDB API key; never persisted"`
	FanartAPIKey  string `header:"X-Heya-Fanart-API-Key" doc:"Optional request-scoped Fanart.tv personal API key; never persisted"`
	AppleAPIKey   string `header:"X-Heya-Apple-API-Key" doc:"Optional request-scoped Apple Music developer token; never persisted"`
	DiscogsAPIKey string `header:"X-Heya-Discogs-API-Key" doc:"Optional request-scoped Discogs token; never persisted"`
	LastFMAPIKey  string `header:"X-Heya-LastFM-API-Key" doc:"Optional request-scoped Last.fm API key; never persisted"`
	Body          struct {
		Kind      string `json:"kind" enum:"movie,artist,release_group"`
		Provider  string `json:"provider" example:"tmdb"`
		Namespace string `json:"namespace" example:"movie"`
		Value     string `json:"value" example:"603"`
	}
}
type resolutionBody struct {
	State    string       `json:"state" enum:"completed,accepted"`
	Entity   any          `json:"entity,omitempty"`
	EntityID string       `json:"entity_id,omitempty"`
	Job      *jobResource `json:"job,omitempty"`
}
type resolutionOutput struct {
	Status int
	Body   resolutionBody
}

type refreshOutput struct {
	Status int
	Body   jobResource
}

type jobInput struct {
	ID int64 `path:"id" minimum:"1"`
}
type jobResource struct {
	ID       int64  `json:"id"`
	Kind     string `json:"kind"`
	State    string `json:"state"`
	EntityID string `json:"entity_id,omitempty"`
	Error    string `json:"error,omitempty"`
}
type jobOutput struct{ Body jobResource }

type searchInput struct {
	Query    string `query:"q" minLength:"1"`
	Kind     string `query:"kind" enum:"movie,artist,release_group,tv_show,anime" doc:"Optional canonical domain filter; TV and Anime are distinct kinds"`
	Limit    int    `query:"limit" minimum:"1" maximum:"100" default:"20"`
	Year     int    `query:"year" minimum:"1800" maximum:"2200"`
	Genre    string `query:"genre"`
	Country  string `query:"country"`
	Language string `query:"language"`
	Status   string `query:"status"`
}
type searchOutput struct {
	Body struct {
		Results []json.RawMessage `json:"results"`
	}
}

type changesInput struct {
	After int64 `query:"after" minimum:"0"`
	Limit int   `query:"limit" minimum:"1" maximum:"500" default:"100"`
}
type changeEntry struct {
	Sequence          int64    `json:"sequence"`
	EntityID          string   `json:"entity_id"`
	EntityKind        string   `json:"entity_kind"`
	Slug              string   `json:"slug"`
	ChangeType        string   `json:"change_type"`
	ChangedScopes     []string `json:"changed_scopes"`
	ProjectionVersion int64    `json:"projection_version"`
	CreatedAt         string   `json:"created_at"`
}
type changesOutput struct {
	Body struct {
		Entries    []changeEntry `json:"entries"`
		NextCursor int64         `json:"next_cursor"`
	}
}

func registerMovies(api huma.API, runtime *platform.Runtime) {
	var service *movies.Service
	var artistService *artists.Service
	var releaseGroupService *releasegroups.Service
	var client *river.Client[pgx.Tx]
	if runtime != nil {
		service = movies.NewService(runtime)
		artistService = artists.NewService(runtime)
		releaseGroupService = releasegroups.NewService(runtime)
		var err error
		client, err = jobs.NewClient(runtime, runtime.Config.Worker.MaxWorkers, false)
		if err != nil {
			panic(err)
		}
	}

	huma.Register(api, huma.Operation{OperationID: "entity-detail", Method: http.MethodGet, Path: "/api/v2/entities/{id}", Summary: "Get a canonical entity", Tags: []string{"Entities"}}, func(ctx context.Context, input *entityInput) (*entityOutput, error) {
		if service == nil {
			return nil, huma.Error503ServiceUnavailable("runtime is unavailable")
		}
		var kind string
		if err := runtime.DB.QueryRow(ctx, `SELECT kind FROM entities WHERE id=$1 AND deleted_at IS NULL`, input.ID).Scan(&kind); err != nil {
			return nil, huma.Error404NotFound("entity not found")
		}
		if kind == "artist" {
			document, fresh, err := artistService.Detail(ctx, input.ID)
			if err != nil {
				return nil, err
			}
			if !fresh {
				if mbid, claimErr := artistService.MusicBrainzID(ctx, input.ID); claimErr == nil {
					if credentialRef, credentialErr := storeProviderCredentials(ctx, runtime, input.TMDBAPIKey, input.OMDBAPIKey, input.TVDBAPIKey, input.FanartAPIKey, input.AppleAPIKey, input.DiscogsAPIKey, input.LastFMAPIKey); credentialErr == nil {
						_, _ = jobs.InsertArtist(ctx, runtime, client, jobs.ArtistIngestArgs{MusicBrainzID: mbid, CredentialRef: credentialRef, Reason: "stale_read"}, jobs.PriorityStaleRead)
					}
				}
				document.Freshness.State = "stale"
			}
			return &entityOutput{Body: document}, nil
		}
		if kind == "release_group" {
			document, fresh, err := releaseGroupService.Detail(ctx, input.ID)
			if err != nil {
				return nil, err
			}
			if !fresh {
				if mbid, claimErr := releaseGroupService.MusicBrainzID(ctx, input.ID); claimErr == nil {
					if credentialRef, credentialErr := storeProviderCredentials(ctx, runtime, input.TMDBAPIKey, input.OMDBAPIKey, input.TVDBAPIKey, input.FanartAPIKey, input.AppleAPIKey, input.DiscogsAPIKey, input.LastFMAPIKey); credentialErr == nil {
						_, _ = jobs.InsertReleaseGroup(ctx, runtime, client, jobs.ReleaseGroupIngestArgs{MusicBrainzID: mbid, CredentialRef: credentialRef, Reason: "stale_read"}, jobs.PriorityStaleRead)
					}
				}
				document.Freshness.State = "stale"
			}
			return &entityOutput{Body: document}, nil
		}
		document, fresh, err := service.Detail(ctx, input.ID)
		if err == movies.ErrNotFound {
			return nil, huma.Error404NotFound("entity not found")
		}
		if err != nil {
			return nil, err
		}
		if !fresh {
			if tmdbID, claimErr := service.TMDBID(ctx, input.ID); claimErr == nil {
				if credentialRef, credentialErr := storeProviderCredentials(ctx, runtime, input.TMDBAPIKey, input.OMDBAPIKey, input.TVDBAPIKey, input.FanartAPIKey, input.AppleAPIKey, input.DiscogsAPIKey, input.LastFMAPIKey); credentialErr == nil {
					_, _ = jobs.InsertMovie(ctx, runtime, client, jobs.MovieIngestArgs{TMDBID: tmdbID, CredentialRef: credentialRef, Reason: "stale_read"}, jobs.PriorityStaleRead)
				}
			}
			document.Freshness.State = "stale"
		}
		return &entityOutput{Body: document}, nil
	})

	huma.Register(api, huma.Operation{OperationID: "resolve-entity", Method: http.MethodPost, Path: "/api/v2/resolutions", Summary: "Resolve or ingest an external entity ID", Tags: []string{"Entities"}, DefaultStatus: http.StatusOK}, func(ctx context.Context, input *resolutionInput) (*resolutionOutput, error) {
		if service == nil || client == nil {
			return nil, huma.Error503ServiceUnavailable("runtime is unavailable")
		}
		if input.Body.Kind == "artist" {
			entityID, err := artistService.Resolve(ctx, input.Body.Provider, input.Body.Namespace, input.Body.Value)
			if err == nil {
				document, _, detailErr := artistService.Detail(ctx, entityID)
				if detailErr != nil {
					return nil, detailErr
				}
				return &resolutionOutput{Status: http.StatusOK, Body: resolutionBody{State: "completed", EntityID: entityID, Entity: document}}, nil
			}
			if err != artists.ErrNotFound {
				return nil, err
			}
			if !strings.EqualFold(input.Body.Provider, "musicbrainz") || !strings.EqualFold(input.Body.Namespace, "artist") {
				return nil, huma.Error404NotFound("external ID is not known and no collector accepts it")
			}
			credentialRef, credentialErr := storeProviderCredentials(ctx, runtime, input.TMDBAPIKey, input.OMDBAPIKey, input.TVDBAPIKey, input.FanartAPIKey, input.AppleAPIKey, input.DiscogsAPIKey, input.LastFMAPIKey)
			if credentialErr != nil {
				return nil, huma.Error503ServiceUnavailable("could not hand provider credentials to worker")
			}
			inserted, insertErr := jobs.InsertArtist(ctx, runtime, client, jobs.ArtistIngestArgs{MusicBrainzID: strings.ToLower(input.Body.Value), CredentialRef: credentialRef, Reason: "interactive_resolution"}, jobs.PriorityInteractive)
			if insertErr != nil {
				return nil, insertErr
			}
			return &resolutionOutput{Status: http.StatusAccepted, Body: resolutionBody{State: "accepted", Job: &jobResource{ID: inserted.Job.ID, Kind: jobs.ArtistIngestKind, State: string(inserted.Job.State)}}}, nil
		}
		if input.Body.Kind == "release_group" {
			entityID, err := releaseGroupService.Resolve(ctx, input.Body.Provider, input.Body.Namespace, input.Body.Value)
			if err == nil {
				document, _, detailErr := releaseGroupService.Detail(ctx, entityID)
				if detailErr != nil {
					return nil, detailErr
				}
				return &resolutionOutput{Status: http.StatusOK, Body: resolutionBody{State: "completed", EntityID: entityID, Entity: document}}, nil
			}
			if err != releasegroups.ErrNotFound {
				return nil, err
			}
			if !strings.EqualFold(input.Body.Provider, "musicbrainz") || !strings.EqualFold(input.Body.Namespace, "release_group") {
				return nil, huma.Error404NotFound("external ID is not known and no collector accepts it")
			}
			credentialRef, credentialErr := storeProviderCredentials(ctx, runtime, input.TMDBAPIKey, input.OMDBAPIKey, input.TVDBAPIKey, input.FanartAPIKey, input.AppleAPIKey, input.DiscogsAPIKey, input.LastFMAPIKey)
			if credentialErr != nil {
				return nil, huma.Error503ServiceUnavailable("could not hand provider credentials to worker")
			}
			inserted, insertErr := jobs.InsertReleaseGroup(ctx, runtime, client, jobs.ReleaseGroupIngestArgs{MusicBrainzID: strings.ToLower(input.Body.Value), CredentialRef: credentialRef, Reason: "interactive_resolution"}, jobs.PriorityInteractive)
			if insertErr != nil {
				return nil, insertErr
			}
			return &resolutionOutput{Status: http.StatusAccepted, Body: resolutionBody{State: "accepted", Job: &jobResource{ID: inserted.Job.ID, Kind: jobs.ReleaseGroupIngestKind, State: string(inserted.Job.State)}}}, nil
		}
		if input.Body.Kind != "movie" {
			return nil, huma.Error400BadRequest("kind must be movie, artist, or release_group")
		}
		entityID, err := service.Resolve(ctx, input.Body.Provider, input.Body.Namespace, input.Body.Value)
		if err == nil {
			document, _, detailErr := service.Detail(ctx, entityID)
			if detailErr != nil {
				return nil, detailErr
			}
			return &resolutionOutput{Status: http.StatusOK, Body: resolutionBody{State: "completed", EntityID: entityID, Entity: document}}, nil
		}
		if err != movies.ErrNotFound {
			return nil, err
		}
		if !strings.EqualFold(input.Body.Provider, "tmdb") || !strings.EqualFold(input.Body.Namespace, "movie") {
			return nil, huma.Error404NotFound("external ID is not known and no collector accepts it")
		}
		tmdbID, parseErr := strconv.ParseInt(input.Body.Value, 10, 64)
		if parseErr != nil || tmdbID < 1 {
			return nil, huma.Error400BadRequest("invalid TMDB movie ID")
		}
		credentialRef, credentialErr := storeProviderCredentials(ctx, runtime, input.TMDBAPIKey, input.OMDBAPIKey, input.TVDBAPIKey, input.FanartAPIKey, input.AppleAPIKey, input.DiscogsAPIKey, input.LastFMAPIKey)
		if credentialErr != nil {
			return nil, huma.Error503ServiceUnavailable("could not hand provider credentials to worker")
		}
		inserted, insertErr := jobs.InsertMovie(ctx, runtime, client, jobs.MovieIngestArgs{TMDBID: tmdbID, CredentialRef: credentialRef, Reason: "interactive_resolution"}, jobs.PriorityInteractive)
		if insertErr != nil {
			return nil, insertErr
		}
		if wait := preferredWait(input.Prefer); wait > 0 {
			deadline := time.NewTimer(wait)
			defer deadline.Stop()
			ticker := time.NewTicker(100 * time.Millisecond)
			defer ticker.Stop()
			for {
				if entityID, resolveErr := service.Resolve(ctx, "tmdb", "movie", input.Body.Value); resolveErr == nil {
					document, _, detailErr := service.Detail(ctx, entityID)
					if detailErr != nil {
						return nil, detailErr
					}
					return &resolutionOutput{Status: http.StatusOK, Body: resolutionBody{State: "completed", EntityID: entityID, Entity: document}}, nil
				}
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-deadline.C:
					goto accepted
				case <-ticker.C:
				}
			}
		}
	accepted:
		return &resolutionOutput{Status: http.StatusAccepted, Body: resolutionBody{State: "accepted", Job: &jobResource{ID: inserted.Job.ID, Kind: jobs.MovieIngestKind, State: string(inserted.Job.State)}}}, nil
	})

	huma.Register(api, huma.Operation{OperationID: "refresh-entity", Method: http.MethodPost, Path: "/api/v2/entities/{id}/refreshes", Summary: "Refresh a canonical entity", Tags: []string{"Entities"}, DefaultStatus: http.StatusAccepted}, func(ctx context.Context, input *entityInput) (*refreshOutput, error) {
		if service == nil || client == nil {
			return nil, huma.Error503ServiceUnavailable("runtime is unavailable")
		}
		var kind string
		if err := runtime.DB.QueryRow(ctx, `SELECT kind FROM entities WHERE id=$1`, input.ID).Scan(&kind); err != nil {
			return nil, huma.Error404NotFound("entity not found")
		}
		if kind == "artist" {
			mbid, err := artistService.MusicBrainzID(ctx, input.ID)
			if err != nil {
				return nil, huma.Error404NotFound("entity has no MusicBrainz artist claim")
			}
			credentialRef, credentialErr := storeProviderCredentials(ctx, runtime, input.TMDBAPIKey, input.OMDBAPIKey, input.TVDBAPIKey, input.FanartAPIKey, input.AppleAPIKey, input.DiscogsAPIKey, input.LastFMAPIKey)
			if credentialErr != nil {
				return nil, huma.Error503ServiceUnavailable("could not hand provider credentials to worker")
			}
			inserted, err := jobs.InsertArtist(ctx, runtime, client, jobs.ArtistIngestArgs{MusicBrainzID: mbid, CredentialRef: credentialRef, Reason: "manual_refresh"}, jobs.PriorityInteractive)
			if err != nil {
				return nil, err
			}
			return &refreshOutput{Status: http.StatusAccepted, Body: jobResource{ID: inserted.Job.ID, Kind: jobs.ArtistIngestKind, State: string(inserted.Job.State)}}, nil
		}
		if kind == "release_group" {
			mbid, err := releaseGroupService.MusicBrainzID(ctx, input.ID)
			if err != nil {
				return nil, huma.Error404NotFound("entity has no MusicBrainz release-group claim")
			}
			credentialRef, credentialErr := storeProviderCredentials(ctx, runtime, input.TMDBAPIKey, input.OMDBAPIKey, input.TVDBAPIKey, input.FanartAPIKey, input.AppleAPIKey, input.DiscogsAPIKey, input.LastFMAPIKey)
			if credentialErr != nil {
				return nil, huma.Error503ServiceUnavailable("could not hand provider credentials to worker")
			}
			inserted, err := jobs.InsertReleaseGroup(ctx, runtime, client, jobs.ReleaseGroupIngestArgs{MusicBrainzID: mbid, CredentialRef: credentialRef, Reason: "manual_refresh"}, jobs.PriorityInteractive)
			if err != nil {
				return nil, err
			}
			return &refreshOutput{Status: http.StatusAccepted, Body: jobResource{ID: inserted.Job.ID, Kind: jobs.ReleaseGroupIngestKind, State: string(inserted.Job.State)}}, nil
		}
		tmdbID, err := service.TMDBID(ctx, input.ID)
		if err == movies.ErrNotFound {
			return nil, huma.Error404NotFound("entity has no TMDB movie claim")
		}
		if err != nil {
			return nil, err
		}
		credentialRef, credentialErr := storeProviderCredentials(ctx, runtime, input.TMDBAPIKey, input.OMDBAPIKey, input.TVDBAPIKey, input.FanartAPIKey, input.AppleAPIKey, input.DiscogsAPIKey, input.LastFMAPIKey)
		if credentialErr != nil {
			return nil, huma.Error503ServiceUnavailable("could not hand provider credentials to worker")
		}
		inserted, err := jobs.InsertMovie(ctx, runtime, client, jobs.MovieIngestArgs{TMDBID: tmdbID, CredentialRef: credentialRef, Reason: "manual_refresh"}, jobs.PriorityInteractive)
		if err != nil {
			return nil, err
		}
		return &refreshOutput{Status: http.StatusAccepted, Body: jobResource{ID: inserted.Job.ID, Kind: jobs.MovieIngestKind, State: string(inserted.Job.State)}}, nil
	})

	huma.Register(api, huma.Operation{OperationID: "job-status", Method: http.MethodGet, Path: "/api/v2/jobs/{id}", Summary: "Get durable job status", Tags: []string{"Jobs"}}, func(ctx context.Context, input *jobInput) (*jobOutput, error) {
		if runtime == nil {
			return nil, huma.Error503ServiceUnavailable("runtime is unavailable")
		}
		var resource jobResource
		resource.ID = input.ID
		if err := runtime.DB.QueryRow(ctx, `SELECT kind, state FROM river_job WHERE id = $1`, input.ID).Scan(&resource.Kind, &resource.State); err != nil {
			return nil, huma.Error404NotFound("job not found")
		}
		var entityID *string
		var failure *string
		_ = runtime.DB.QueryRow(ctx, `SELECT entity_id, error FROM movie_ingestion_runs WHERE river_job_id = $1`, input.ID).Scan(&entityID, &failure)
		if entityID == nil && failure == nil {
			_ = runtime.DB.QueryRow(ctx, `SELECT entity_id,error FROM artist_ingestion_runs WHERE river_job_id=$1`, input.ID).Scan(&entityID, &failure)
		}
		if entityID == nil && failure == nil {
			_ = runtime.DB.QueryRow(ctx, `SELECT entity_id,error FROM release_group_ingestion_runs WHERE river_job_id=$1`, input.ID).Scan(&entityID, &failure)
		}
		if entityID != nil {
			resource.EntityID = *entityID
		}
		if failure != nil {
			resource.Error = *failure
		}
		return &jobOutput{Body: resource}, nil
	})

	huma.Register(api, huma.Operation{OperationID: "search-entities", Method: http.MethodGet, Path: "/api/v2/search", Summary: "Search canonical entities", Tags: []string{"Search"}}, func(ctx context.Context, input *searchInput) (*searchOutput, error) {
		if service == nil {
			return nil, huma.Error503ServiceUnavailable("runtime is unavailable")
		}
		results, err := searchAllEntities(ctx, runtime, input)
		if err != nil {
			return nil, fmt.Errorf("search entities: %w", err)
		}
		output := &searchOutput{}
		output.Body.Results = results
		return output, nil
	})

	huma.Register(api, huma.Operation{OperationID: "public-changes", Method: http.MethodGet, Path: "/api/v2/changes", Summary: "Read the gap-free public change feed", Tags: []string{"Changes"}}, func(ctx context.Context, input *changesInput) (*changesOutput, error) {
		if runtime == nil {
			return nil, huma.Error503ServiceUnavailable("runtime is unavailable")
		}
		limit := input.Limit
		if limit < 1 {
			limit = 100
		}
		rows, err := runtime.DB.Query(ctx, `SELECT sequence,entity_id,entity_kind,slug,change_type,changed_scopes,projection_version,created_at FROM change_log WHERE sequence > $1 AND scope='public' ORDER BY sequence LIMIT $2`, input.After, limit)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		output := &changesOutput{}
		output.Body.NextCursor = input.After
		for rows.Next() {
			var entry changeEntry
			var createdAt time.Time
			if err := rows.Scan(&entry.Sequence, &entry.EntityID, &entry.EntityKind, &entry.Slug, &entry.ChangeType, &entry.ChangedScopes, &entry.ProjectionVersion, &createdAt); err != nil {
				return nil, err
			}
			entry.CreatedAt = createdAt.UTC().Format(time.RFC3339Nano)
			output.Body.Entries = append(output.Body.Entries, entry)
			output.Body.NextCursor = entry.Sequence
		}
		return output, rows.Err()
	})
}

func searchAllEntities(ctx context.Context, runtime *platform.Runtime, input *searchInput) ([]json.RawMessage, error) {
	query := strings.TrimSpace(input.Query)
	if query == "" {
		return nil, nil
	}
	limit := input.Limit
	if limit < 1 || limit > 100 {
		limit = 20
	}
	kind := strings.ToLower(strings.TrimSpace(input.Kind))
	cacheInput, _ := json.Marshal([]any{strings.ToLower(query), kind, limit, input.Year, strings.ToLower(input.Genre), strings.ToUpper(input.Country), strings.ToLower(input.Language), strings.ToLower(input.Status)})
	digest := sha256.Sum256(cacheInput)
	cacheKey := "heya:metadata:v2:search:" + hex.EncodeToString(digest[:])
	if cached, err := runtime.Redis.Get(ctx, cacheKey).Bytes(); err == nil {
		var result []json.RawMessage
		if json.Unmarshal(cached, &result) == nil {
			return result, nil
		}
	}
	provider := ""
	value := query
	if parts := strings.SplitN(query, ":", 2); len(parts) == 2 {
		provider = strings.ToLower(parts[0])
		value = parts[1]
	}
	rows, err := runtime.DB.Query(ctx, `WITH matches AS (
		SELECT entity_id,0 AS tier,1::double precision AS score FROM external_id_claims WHERE state='accepted' AND lower(normalized_value)=lower($1) AND ($2='' OR provider=$2)
		UNION ALL
		SELECT entity_id,CASE WHEN normalized_value=lower(unaccent($3)) THEN 1 WHEN normalized_value LIKE lower(unaccent($3))||'%' THEN 2 ELSE 3 END,similarity(normalized_value,lower(unaccent($3))) FROM search_names WHERE normalized_value=lower(unaccent($3)) OR normalized_value LIKE lower(unaccent($3))||'%' OR similarity(normalized_value,lower(unaccent($3)))>=0.25
	), ranked AS (SELECT entity_id,min(tier) tier,max(score) score FROM matches GROUP BY entity_id)
	SELECT se.summary FROM ranked JOIN search_entities se ON se.entity_id=ranked.entity_id
	WHERE ($5=0 OR se.release_year=$5) AND ($6='' OR EXISTS(SELECT 1 FROM unnest(se.genres) genre WHERE lower(genre)=lower($6))) AND ($7='' OR upper($7)=ANY(se.countries)) AND ($8='' OR lower($8)=ANY(se.languages)) AND ($9='' OR se.status=lower($9)) AND ($10='' OR se.kind=$10)
	ORDER BY ranked.tier,ranked.score DESC,se.popularity DESC NULLS LAST,se.display_title LIMIT $4`, value, provider, query, limit, input.Year, input.Genre, input.Country, input.Language, input.Status, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []json.RawMessage
	for rows.Next() {
		var body []byte
		if err := rows.Scan(&body); err != nil {
			return nil, err
		}
		result = append(result, json.RawMessage(body))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	ttl := 2 * time.Minute
	if len(result) == 0 {
		ttl = 15 * time.Second
	}
	if body, err := json.Marshal(result); err == nil {
		_ = runtime.Redis.Set(ctx, cacheKey, body, ttl).Err()
	}
	return result, nil
}

func storeProviderCredentials(ctx context.Context, runtime *platform.Runtime, tmdbAPIKey, omdbAPIKey, tvdbAPIKey, fanartAPIKey, appleAPIKey, discogsAPIKey, lastFMAPIKey string) (string, error) {
	apiKeys := map[string]string{}
	if value := strings.TrimSpace(tmdbAPIKey); value != "" {
		apiKeys["tmdb"] = value
	}
	if value := strings.TrimSpace(omdbAPIKey); value != "" {
		apiKeys["omdb"] = value
	}
	if value := strings.TrimSpace(tvdbAPIKey); value != "" {
		apiKeys["tvdb"] = value
	}
	if value := strings.TrimSpace(fanartAPIKey); value != "" {
		apiKeys["fanart"] = value
	}
	if value := strings.TrimSpace(appleAPIKey); value != "" {
		apiKeys["apple"] = value
	}
	if value := strings.TrimSpace(discogsAPIKey); value != "" {
		apiKeys["discogs"] = value
	}
	if value := strings.TrimSpace(lastFMAPIKey); value != "" {
		apiKeys["lastfm"] = value
	}
	if len(apiKeys) == 0 {
		return "", nil
	}
	return providercredentials.Store(ctx, runtime.Redis, providercredentials.Credentials{
		APIKeys: apiKeys,
	})
}

func preferredWait(header string) time.Duration {
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(part)
		if !strings.HasPrefix(part, "wait=") {
			continue
		}
		seconds, err := strconv.Atoi(strings.TrimPrefix(part, "wait="))
		if err != nil || seconds < 1 {
			return 0
		}
		if seconds > 5 {
			seconds = 5
		}
		return time.Duration(seconds) * time.Second
	}
	return 0
}

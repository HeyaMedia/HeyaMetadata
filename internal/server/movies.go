package server

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	moviedomain "github.com/HeyaMedia/HeyaMetadata/internal/domains/movie"
	"github.com/HeyaMedia/HeyaMetadata/internal/jobs"
	"github.com/HeyaMedia/HeyaMetadata/internal/movies"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
)

type entityInput struct {
	ID string `path:"id" format:"uuid"`
}
type entityOutput struct{ Body moviedomain.DetailDocument }

type resolutionInput struct {
	Prefer string `header:"Prefer"`
	Body   struct {
		Kind      string `json:"kind" enum:"movie"`
		Provider  string `json:"provider" example:"tmdb"`
		Namespace string `json:"namespace" example:"movie"`
		Value     string `json:"value" example:"603"`
	}
}
type resolutionBody struct {
	State    string                      `json:"state" enum:"completed,accepted"`
	Entity   *moviedomain.DetailDocument `json:"entity,omitempty"`
	EntityID string                      `json:"entity_id,omitempty"`
	Job      *jobResource                `json:"job,omitempty"`
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
	Limit    int    `query:"limit" minimum:"1" maximum:"100" default:"20"`
	Year     int    `query:"year" minimum:"1800" maximum:"2200"`
	Genre    string `query:"genre"`
	Country  string `query:"country"`
	Language string `query:"language"`
	Status   string `query:"status"`
}
type searchOutput struct {
	Body struct {
		Results []moviedomain.SummaryDocument `json:"results"`
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
	var client *river.Client[pgx.Tx]
	if runtime != nil {
		service = movies.NewService(runtime)
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
		document, fresh, err := service.Detail(ctx, input.ID)
		if err == movies.ErrNotFound {
			return nil, huma.Error404NotFound("entity not found")
		}
		if err != nil {
			return nil, err
		}
		if !fresh {
			if tmdbID, claimErr := service.TMDBID(ctx, input.ID); claimErr == nil {
				_, _ = client.Insert(ctx, jobs.MovieIngestArgs{TMDBID: tmdbID}, nil)
			}
			document.Freshness.State = "stale"
		}
		return &entityOutput{Body: document}, nil
	})

	huma.Register(api, huma.Operation{OperationID: "resolve-entity", Method: http.MethodPost, Path: "/api/v2/resolutions", Summary: "Resolve or ingest an external entity ID", Tags: []string{"Entities"}, DefaultStatus: http.StatusOK}, func(ctx context.Context, input *resolutionInput) (*resolutionOutput, error) {
		if service == nil || client == nil {
			return nil, huma.Error503ServiceUnavailable("runtime is unavailable")
		}
		if input.Body.Kind != "movie" {
			return nil, huma.Error400BadRequest("only movie resolution is implemented")
		}
		entityID, err := service.Resolve(ctx, input.Body.Provider, input.Body.Namespace, input.Body.Value)
		if err == nil {
			document, _, detailErr := service.Detail(ctx, entityID)
			if detailErr != nil {
				return nil, detailErr
			}
			return &resolutionOutput{Status: http.StatusOK, Body: resolutionBody{State: "completed", EntityID: entityID, Entity: &document}}, nil
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
		inserted, insertErr := client.Insert(ctx, jobs.MovieIngestArgs{TMDBID: tmdbID}, nil)
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
					return &resolutionOutput{Status: http.StatusOK, Body: resolutionBody{State: "completed", EntityID: entityID, Entity: &document}}, nil
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
		tmdbID, err := service.TMDBID(ctx, input.ID)
		if err == movies.ErrNotFound {
			return nil, huma.Error404NotFound("entity has no TMDB movie claim")
		}
		if err != nil {
			return nil, err
		}
		inserted, err := client.Insert(ctx, jobs.MovieIngestArgs{TMDBID: tmdbID}, nil)
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
		results, err := service.Search(ctx, input.Query, movies.SearchFilters{Year: input.Year, Genre: input.Genre, Country: input.Country, Language: input.Language, Status: input.Status}, input.Limit)
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

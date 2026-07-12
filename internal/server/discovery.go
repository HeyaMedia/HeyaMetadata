package server

import (
	"context"
	"github.com/HeyaMedia/HeyaMetadata/internal/discovery"
	"github.com/HeyaMedia/HeyaMetadata/internal/jobs"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/HeyaMedia/HeyaMetadata/internal/providercredentials"
	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"net/http"
	"time"
)

type discoveryCreateInput struct {
	Prefer     string `header:"Prefer" doc:"respond-async or wait=N (maximum 5 seconds)"`
	TMDBAPIKey string `header:"X-Heya-TMDB-API-Key" doc:"Optional request-scoped TMDB API key; never persisted"`
	Body       discovery.Request
}
type dedicatedDiscoveryRequest struct {
	Query string          `json:"query"`
	Limit int             `json:"limit,omitempty"`
	Hints discovery.Hints `json:"hints,omitempty"`
}
type dedicatedDiscoveryInput struct {
	Prefer string `header:"Prefer" doc:"respond-async or wait=N (maximum 5 seconds)"`
	Body   dedicatedDiscoveryRequest
}
type discoveryGetInput struct {
	ID string `path:"id" format:"uuid"`
}
type discoveryResource struct {
	ID        string            `json:"id"`
	State     string            `json:"state" enum:"queued,working,completed,failed"`
	Result    *discovery.Result `json:"result,omitempty"`
	Job       *jobResource      `json:"job,omitempty"`
	Error     string            `json:"error,omitempty"`
	ExpiresAt time.Time         `json:"expires_at"`
}
type discoveryOutput struct {
	Status int
	Body   discoveryResource
}

func registerDiscovery(api huma.API, runtime *platform.Runtime) {
	var client *river.Client[pgx.Tx]
	if runtime != nil {
		var err error
		client, err = jobs.NewClient(runtime, runtime.Config.Worker.MaxWorkers, false)
		if err != nil {
			panic(err)
		}
	}
	create := func(ctx context.Context, request discovery.Request, prefer, tmdbAPIKey string) (*discoveryOutput, error) {
		if runtime == nil || client == nil {
			return nil, huma.Error503ServiceUnavailable("runtime is unavailable")
		}
		request = discovery.NormalizeRequest(request)
		if request.Kind != discovery.KindArtist && request.Kind != discovery.KindMovie && request.Kind != discovery.KindReleaseGroup && request.Kind != discovery.KindRecording && request.Kind != discovery.KindTVShow && request.Kind != discovery.KindAnime && request.Kind != discovery.KindBookWork {
			return nil, huma.Error400BadRequest("unsupported discovery kind")
		}
		run, err := discovery.EnsureRun(ctx, runtime, request)
		if err != nil {
			return nil, err
		}
		if run.State == "completed" && run.Result != nil {
			return &discoveryOutput{Status: http.StatusOK, Body: discoveryRunResource(run)}, nil
		}
		if run.State == "queued" {
			credentials := providercredentials.Credentials{}
			if tmdbAPIKey != "" {
				credentials.APIKeys = map[string]string{"tmdb": tmdbAPIKey}
			}
			credentialRef, credentialErr := providercredentials.Store(ctx, runtime.Redis, credentials)
			if credentialErr != nil {
				return nil, huma.Error503ServiceUnavailable("could not hand provider credentials to worker")
			}
			inserted, insertErr := jobs.InsertDiscovery(ctx, runtime, client, run, credentialRef)
			if insertErr != nil {
				_ = providercredentials.Delete(context.WithoutCancel(ctx), runtime.Redis, credentialRef)
				return nil, insertErr
			}
			run.RiverJobID = inserted.Job.ID
		}
		wait := 1200 * time.Millisecond
		if preferredWait(prefer) > 0 {
			wait = preferredWait(prefer)
		}
		if prefer == "respond-async" {
			wait = 0
		}
		if wait > 0 {
			timer := time.NewTimer(wait)
			defer timer.Stop()
			ticker := time.NewTicker(40 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-timer.C:
					goto accepted
				case <-ticker.C:
					current, getErr := discovery.GetRun(ctx, runtime, run.ID)
					if getErr == nil && (current.State == "completed" || current.State == "failed") {
						status := http.StatusOK
						if current.State == "failed" {
							status = http.StatusUnprocessableEntity
						}
						return &discoveryOutput{Status: status, Body: discoveryRunResource(current)}, nil
					}
				}
			}
		}
	accepted:
		return &discoveryOutput{Status: http.StatusAccepted, Body: discoveryRunResource(run)}, nil
	}
	huma.Register(api, huma.Operation{OperationID: "create-discovery", Method: http.MethodPost, Path: "/api/v2/discoveries", Summary: "Discover and rank upstream identity candidates", Description: "Searches providers only when an entity is not yet known. Structured hints produce explainable, resolution-ready candidates for every current canonical domain.", Tags: []string{"Discovery"}, DefaultStatus: http.StatusOK}, func(ctx context.Context, input *discoveryCreateInput) (*discoveryOutput, error) {
		return create(ctx, input.Body, input.Prefer, input.TMDBAPIKey)
	})
	huma.Register(api, huma.Operation{OperationID: "discover-tv-show", Method: http.MethodPost, Path: "/api/v2/tv/discoveries", Summary: "Discover conventional television shows", Tags: []string{"TV", "Discovery"}, DefaultStatus: http.StatusOK}, func(ctx context.Context, input *dedicatedDiscoveryInput) (*discoveryOutput, error) {
		return create(ctx, discovery.Request{Kind: discovery.KindTVShow, Query: input.Body.Query, Limit: input.Body.Limit, Hints: input.Body.Hints}, input.Prefer, "")
	})
	huma.Register(api, huma.Operation{OperationID: "discover-anime", Method: http.MethodPost, Path: "/api/v2/anime/discoveries", Summary: "Discover AniDB anime identities", Tags: []string{"Anime", "Discovery"}, DefaultStatus: http.StatusOK}, func(ctx context.Context, input *dedicatedDiscoveryInput) (*discoveryOutput, error) {
		return create(ctx, discovery.Request{Kind: discovery.KindAnime, Query: input.Body.Query, Limit: input.Body.Limit, Hints: input.Body.Hints}, input.Prefer, "")
	})
	huma.Register(api, huma.Operation{OperationID: "get-discovery", Method: http.MethodGet, Path: "/api/v2/discoveries/{id}", Summary: "Get smart-discovery status and candidates", Tags: []string{"Discovery"}}, func(ctx context.Context, input *discoveryGetInput) (*discoveryOutput, error) {
		if runtime == nil {
			return nil, huma.Error503ServiceUnavailable("runtime is unavailable")
		}
		run, err := discovery.GetRun(ctx, runtime, input.ID)
		if err == pgx.ErrNoRows {
			return nil, huma.Error404NotFound("discovery not found")
		}
		if err != nil {
			return nil, err
		}
		return &discoveryOutput{Status: http.StatusOK, Body: discoveryRunResource(run)}, nil
	})
}
func discoveryRunResource(run discovery.Run) discoveryResource {
	resource := discoveryResource{ID: run.ID, State: run.State, Result: run.Result, Error: run.Error, ExpiresAt: run.ExpiresAt}
	if run.RiverJobID > 0 {
		resource.Job = &jobResource{ID: run.RiverJobID, Kind: jobs.DiscoverySearchKind, State: run.State}
	}
	return resource
}

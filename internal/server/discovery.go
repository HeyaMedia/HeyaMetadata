package server

import (
	"context"
	"github.com/HeyaMedia/HeyaMetadata/internal/discovery"
	"github.com/HeyaMedia/HeyaMetadata/internal/jobs"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"net/http"
	"time"
)

type discoveryCreateInput struct {
	Prefer string `header:"Prefer" doc:"respond-async or wait=N (maximum 5 seconds)"`
	Body   discovery.Request
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
	huma.Register(api, huma.Operation{OperationID: "create-discovery", Method: http.MethodPost, Path: "/api/v2/discoveries", Summary: "Discover and rank upstream identity candidates", Description: "Searches providers only when an entity is not yet known. Structured hints produce explainable, resolution-ready candidates. TV and Anime are distinct discovery kinds; artist is the first implemented provider route.", Tags: []string{"Discovery"}, DefaultStatus: http.StatusOK}, func(ctx context.Context, input *discoveryCreateInput) (*discoveryOutput, error) {
		if runtime == nil || client == nil {
			return nil, huma.Error503ServiceUnavailable("runtime is unavailable")
		}
		request := discovery.NormalizeRequest(input.Body)
		if request.Kind != discovery.KindArtist {
			return nil, huma.Error400BadRequest("artist discovery is implemented; movie, tv_show, anime, and release_group routing is pending")
		}
		run, err := discovery.EnsureRun(ctx, runtime, request)
		if err != nil {
			return nil, err
		}
		if run.State == "completed" && run.Result != nil {
			return &discoveryOutput{Status: http.StatusOK, Body: discoveryRunResource(run)}, nil
		}
		if run.State == "queued" {
			inserted, insertErr := jobs.InsertDiscovery(ctx, runtime, client, run)
			if insertErr != nil {
				return nil, insertErr
			}
			run.RiverJobID = inserted.Job.ID
		}
		wait := 1200 * time.Millisecond
		if preferredWait(input.Prefer) > 0 {
			wait = preferredWait(input.Prefer)
		}
		if input.Prefer == "respond-async" {
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

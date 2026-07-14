package server

import (
	"context"
	"net/http"

	"github.com/HeyaMedia/HeyaMetadata/internal/jobs"
	"github.com/HeyaMedia/HeyaMetadata/internal/people"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
)

type canonicalPersonCreditsInput struct {
	ID         string `path:"id" format:"uuid"`
	Offset     int    `query:"offset" minimum:"0" default:"0"`
	Limit      int    `query:"limit" minimum:"1" maximum:"250" default:"100"`
	TMDBAPIKey string `header:"X-Heya-TMDB-API-Key" doc:"Optional request-scoped TMDB API key; never persisted"`
	TVDBAPIKey string `header:"X-Heya-TVDB-API-Key" doc:"Optional request-scoped TVDB API key; never persisted"`
}
type personSummary struct {
	EntityID       string `json:"entity_id" format:"uuid"`
	DisplayName    string `json:"display_name"`
	ProfileImageID string `json:"profile_image_id,omitempty" format:"uuid"`
}
type personCreditsOutput struct {
	Body struct {
		Person  personSummary         `json:"person"`
		Credits []people.PersonCredit `json:"credits"`
		Total   int                   `json:"total"`
		Offset  int                   `json:"offset"`
		Limit   int                   `json:"limit"`
	}
}

type canonicalPersonInput struct {
	ID         string `path:"id" format:"uuid"`
	TMDBAPIKey string `header:"X-Heya-TMDB-API-Key" doc:"Optional request-scoped TMDB API key; never persisted"`
	TVDBAPIKey string `header:"X-Heya-TVDB-API-Key" doc:"Optional request-scoped TVDB API key; never persisted"`
}
type canonicalPersonOutput struct{ Body people.PersonDocument }

func registerPersons(api huma.API, runtime *platform.Runtime) {
	var service *people.Service
	var client *river.Client[pgx.Tx]
	if runtime != nil {
		service = people.NewService(runtime)
		client, _ = jobs.NewClient(runtime, runtime.Config.Worker.MaxWorkers, false)
	}
	huma.Register(api, huma.Operation{OperationID: "person-detail", Method: http.MethodGet, Path: "/api/v2/persons/{id}", Summary: "Get a canonical person and combined filmography", Tags: []string{"People", "Credits"}}, func(ctx context.Context, input *canonicalPersonInput) (*canonicalPersonOutput, error) {
		if service == nil {
			return nil, huma.Error503ServiceUnavailable("runtime is unavailable")
		}
		document, err := service.Detail(ctx, input.ID)
		if err == people.ErrNotFound {
			return nil, huma.Error404NotFound("person not found")
		}
		if err != nil {
			return nil, err
		}
		if ids, refreshErr := service.DueProviderIDs(ctx, document.ID); refreshErr == nil && len(ids) > 0 && client != nil {
			credentialRef, credentialErr := storeProviderCredentials(ctx, runtime, input.TMDBAPIKey, "", input.TVDBAPIKey, "", "", "", "")
			if credentialErr == nil {
				_, _ = jobs.InsertPersonEnrich(ctx, runtime, client, personEnrichArgs(document.ID, ids, credentialRef, "stale_read"), jobs.PriorityStaleRead)
				document.Freshness.State = "stale"
			}
		}
		return &canonicalPersonOutput{Body: document}, nil
	})
	huma.Register(api, huma.Operation{OperationID: "person-credits", Method: http.MethodGet, Path: "/api/v2/persons/{id}/credits", Summary: "Get complete filmography for a canonical person", Description: "All control flow uses the canonical Heya person ID; upstream person IDs remain passive provenance.", Tags: []string{"People", "Credits"}}, func(ctx context.Context, input *canonicalPersonCreditsInput) (*personCreditsOutput, error) {
		if service == nil {
			return nil, huma.Error503ServiceUnavailable("runtime is unavailable")
		}
		entityID, err := service.CanonicalID(ctx, input.ID)
		if err == people.ErrNotFound {
			return nil, huma.Error404NotFound("person credits not found")
		}
		if err != nil {
			return nil, err
		}
		document, err := service.Detail(ctx, entityID)
		if err != nil {
			return nil, err
		}
		if ids, refreshErr := service.DueProviderIDs(ctx, entityID); refreshErr == nil && len(ids) > 0 && client != nil {
			credentialRef, credentialErr := storeProviderCredentials(ctx, runtime, input.TMDBAPIKey, "", input.TVDBAPIKey, "", "", "", "")
			if credentialErr == nil {
				_, _ = jobs.InsertPersonEnrich(ctx, runtime, client, personEnrichArgs(entityID, ids, credentialRef, "stale_read"), jobs.PriorityStaleRead)
			}
		}
		offset, limit := metadataPage(input.Offset, input.Limit)
		out := &personCreditsOutput{}
		out.Body.Offset, out.Body.Limit = offset, limit
		out.Body.Person = personSummary{EntityID: document.ID, DisplayName: document.Display.Title, ProfileImageID: document.Display.ImageID}
		out.Body.Credits, out.Body.Total, err = service.Credits(ctx, entityID, offset, limit)
		if err != nil {
			return nil, err
		}
		return out, nil
	})
}

func personEnrichArgs(entityID string, ids map[string]string, credentialRef, reason string) jobs.PersonEnrichArgs {
	return jobs.PersonEnrichArgs{
		EntityID:      entityID,
		TMDBID:        ids["tmdb"],
		TVMazeID:      ids["tvmaze"],
		TVDBID:        ids["tvdb"],
		CredentialRef: credentialRef,
		Reason:        reason,
	}
}

package server

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/images"
	"github.com/HeyaMedia/HeyaMetadata/internal/jobs"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
)

type imageInput struct {
	ID string `path:"id" format:"uuid"`
}
type imageVariantInput struct {
	ID     string `path:"id" format:"uuid"`
	Format string `path:"format" enum:"webp"`
	Width  int    `path:"width" minimum:"64" maximum:"3840"`
}
type entityImagesInput struct {
	ID                string `path:"id" format:"uuid"`
	Class             string `query:"class" doc:"Optional artwork class such as poster, backdrop, logo, banner, cover, or profile"`
	Language          string `query:"language" doc:"Preferred BCP 47 artwork language, for example en-GB"`
	FallbackLanguages string `query:"fallback_languages" doc:"Comma-separated ordered language fallbacks"`
	AcceptLanguage    string `header:"Accept-Language" doc:"Used after explicit language query preferences"`
	Country           string `query:"country" minLength:"2" maxLength:"2" doc:"Optional ISO 3166-1 alpha-2 regional preference"`
	Limit             int    `query:"limit" minimum:"1" maximum:"100" default:"25" doc:"Maximum candidates returned per artwork class"`
}
type entityImagesOutput struct {
	Vary         string `header:"Vary"`
	ServerTiming string `header:"Server-Timing"`
	Body         struct {
		LanguagePreferences []string                      `json:"language_preferences"`
		Selections          map[string]string             `json:"selections"`
		Results             []images.EntityImageCandidate `json:"results"`
	}
}
type imageOutput struct {
	Status       int
	ContentType  string `header:"Content-Type"`
	CacheControl string `header:"Cache-Control"`
	ETag         string `header:"ETag"`
	ImageWidth   int    `header:"X-Image-Width"`
	ImageHeight  int    `header:"X-Image-Height"`
	RetryAfter   string `header:"Retry-After"`
	Body         []byte
}

func registerImages(api huma.API, runtime *platform.Runtime) {
	var service *images.Service
	var client *river.Client[pgx.Tx]
	if runtime != nil {
		service = images.NewService(runtime)
		var err error
		client, err = jobs.NewClient(runtime, runtime.Config.Worker.MaxWorkers, false)
		if err != nil {
			panic(err)
		}
	}
	imageResponses := func(mediaTypes ...string) map[string]*huma.Response {
		return map[string]*huma.Response{
			"200": binaryResponse("Canonical image bytes", mediaTypes...),
			"202": acceptedJSONResponse("#/components/schemas/JobResource"),
		}
	}
	huma.Register(api, huma.Operation{OperationID: "image-original", Method: http.MethodGet, Path: "/api/v2/images/{id}", Summary: "Read or queue a canonical image original", Tags: []string{"Images"}, DefaultStatus: http.StatusOK, Responses: imageResponses("image/jpeg", "image/png", "image/webp", "image/gif")}, func(ctx context.Context, input *imageInput) (*imageOutput, error) {
		if service == nil {
			return nil, huma.Error503ServiceUnavailable("runtime is unavailable")
		}
		asset, body, err := service.Read(ctx, input.ID)
		if err == nil {
			return readyImageOutput(asset, body), nil
		}
		if errors.Is(err, images.ErrNotFound) {
			return nil, huma.Error404NotFound("image not found")
		}
		if !errors.Is(err, images.ErrNotReady) {
			return nil, err
		}
		inserted, insertErr := client.Insert(ctx, jobs.ImageMaterializeArgs{ImageID: input.ID}, nil)
		if insertErr != nil {
			return nil, insertErr
		}
		payload, _ := json.Marshal(jobResource{ID: inserted.Job.ID, Kind: jobs.ImageMaterializeKind, State: string(inserted.Job.State)})
		return &imageOutput{Status: http.StatusAccepted, ContentType: "application/json", CacheControl: "no-store", RetryAfter: "1", Body: payload}, nil
	})
	huma.Register(api, huma.Operation{OperationID: "image-variant", Method: http.MethodGet, Path: "/api/v2/images/{id}/variants/{format}/{width}", Summary: "Read or queue an optimized image variant", Tags: []string{"Images"}, DefaultStatus: http.StatusOK, Responses: imageResponses("image/webp")}, func(ctx context.Context, input *imageVariantInput) (*imageOutput, error) {
		if service == nil {
			return nil, huma.Error503ServiceUnavailable("runtime is unavailable")
		}
		asset, body, err := service.ReadVariant(ctx, input.ID, input.Width)
		if err == nil {
			return readyImageOutput(asset, body), nil
		}
		if errors.Is(err, images.ErrNotFound) {
			return nil, huma.Error404NotFound("image not found")
		}
		if !errors.Is(err, images.ErrNotReady) {
			return nil, err
		}
		inserted, insertErr := client.Insert(ctx, jobs.ImageVariantArgs{ImageID: input.ID, Width: images.CanonicalVariantWidth(input.Width)}, nil)
		if insertErr != nil {
			return nil, insertErr
		}
		payload, _ := json.Marshal(jobResource{ID: inserted.Job.ID, Kind: jobs.ImageVariantKind, State: string(inserted.Job.State)})
		return &imageOutput{Status: http.StatusAccepted, ContentType: "application/json", CacheControl: "no-store", RetryAfter: "1", Body: payload}, nil
	})
	huma.Register(api, huma.Operation{OperationID: "entity-images", Method: http.MethodGet, Path: "/api/v2/entities/{id}/images", Summary: "Select language-aware artwork for an entity", Description: "Ranks artwork within each class by requested language, neutral fallback, country, provider score, and dimensions. The selected candidate for every returned class is also exposed in selections so clients do not need to reproduce ranking rules.", Tags: []string{"Entities", "Images"}}, func(ctx context.Context, input *entityImagesInput) (output *entityImagesOutput, returnErr error) {
		started := time.Now()
		defer func() {
			duration := time.Since(started)
			if output != nil {
				output.ServerTiming = serverTiming("images", duration)
			}
			returned := 0
			if output != nil {
				returned = len(output.Body.Results)
			}
			slog.InfoContext(ctx, "entity images read", "entity_id", input.ID, "class", input.Class, "returned_rows", returned, "duration_ms", duration.Milliseconds(), "error", returnErr)
		}()
		if service == nil {
			return nil, huma.Error503ServiceUnavailable("runtime is unavailable")
		}
		var exists bool
		if err := runtime.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM entities WHERE id=$1 AND deleted_at IS NULL)`, input.ID).Scan(&exists); err != nil {
			return nil, err
		}
		if !exists {
			return nil, huma.Error404NotFound("entity not found")
		}
		candidates, err := service.Candidates(ctx, input.ID, input.Class)
		if err != nil {
			return nil, err
		}
		preferences := images.LanguagePreferences(input.Language, input.FallbackLanguages, input.AcceptLanguage)
		candidates = images.RankCandidates(candidates, preferences, input.Country)
		output = &entityImagesOutput{Vary: "Accept-Language"}
		output.Body.LanguagePreferences = preferences
		output.Body.Selections = map[string]string{}
		perClass := map[string]int{}
		for _, candidate := range candidates {
			if candidate.Selected {
				output.Body.Selections[candidate.Class] = candidate.ID
			}
			if perClass[candidate.Class] >= input.Limit {
				continue
			}
			perClass[candidate.Class]++
			output.Body.Results = append(output.Body.Results, candidate)
		}
		if output.Body.Results == nil {
			output.Body.Results = []images.EntityImageCandidate{}
		}
		return output, nil
	})
}

func readyImageOutput(asset images.Asset, body []byte) *imageOutput {
	etag := ""
	if asset.Checksum != "" {
		etag = `"sha256-` + asset.Checksum + `"`
	}
	return &imageOutput{
		Status: http.StatusOK, ContentType: asset.MediaType,
		CacheControl: "public, max-age=604800, stale-while-revalidate=2592000",
		ETag:         etag, ImageWidth: asset.Width, ImageHeight: asset.Height, Body: body,
	}
}

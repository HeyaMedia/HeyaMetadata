package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

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
type imageOutput struct {
	Status       int
	ContentType  string `header:"Content-Type"`
	CacheControl string `header:"Cache-Control"`
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
	huma.Register(api, huma.Operation{OperationID: "image-original", Method: http.MethodGet, Path: "/api/v2/images/{id}", Summary: "Read or queue a canonical image original", Tags: []string{"Images"}, DefaultStatus: http.StatusOK}, func(ctx context.Context, input *imageInput) (*imageOutput, error) {
		if service == nil {
			return nil, huma.Error503ServiceUnavailable("runtime is unavailable")
		}
		asset, body, err := service.Read(ctx, input.ID)
		if err == nil {
			return &imageOutput{Status: http.StatusOK, ContentType: asset.MediaType, CacheControl: "public, max-age=31536000, immutable", Body: body}, nil
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
		return &imageOutput{Status: http.StatusAccepted, ContentType: "application/json", CacheControl: "no-store", RetryAfter: "2", Body: payload}, nil
	})
}

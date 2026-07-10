package server

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
)

type Health struct {
	Status  string `json:"status" example:"ok"`
	Service string `json:"service" example:"heya-metadata"`
	Version string `json:"version" example:"dev"`
}

type healthOutput struct {
	Body Health
}

func registerHealth(api huma.API, version string) {
	registerHealthEndpoint(api, "/api/v2/health/live", "health-live", "Liveness probe", version)
	registerHealthEndpoint(api, "/api/v2/health/ready", "health-ready", "Readiness probe", version)
}

func registerHealthEndpoint(api huma.API, path, operationID, summary, version string) {
	huma.Register(api, huma.Operation{
		OperationID: operationID,
		Method:      http.MethodGet,
		Path:        path,
		Summary:     summary,
		Tags:        []string{"System"},
	}, func(_ context.Context, _ *struct{}) (*healthOutput, error) {
		return &healthOutput{Body: Health{
			Status:  "ok",
			Service: "heya-metadata",
			Version: version,
		}}, nil
	})
}

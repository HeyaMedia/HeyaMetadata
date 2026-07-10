package server

import (
	"context"
	"log/slog"
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

type ReadinessChecker interface {
	Check(context.Context) map[string]error
}

type DependencyHealth struct {
	Status string `json:"status" example:"ok" enum:"ok,unavailable"`
}

type Readiness struct {
	Status       string                      `json:"status" example:"ok" enum:"ok,not_ready"`
	Service      string                      `json:"service" example:"heya-metadata"`
	Version      string                      `json:"version" example:"dev"`
	Dependencies map[string]DependencyHealth `json:"dependencies"`
}

type readinessOutput struct {
	Status int
	Body   Readiness
}

func registerHealth(api huma.API, version string, checker ReadinessChecker) {
	registerLiveness(api, version)
	registerReadiness(api, version, checker)
}

func registerLiveness(api huma.API, version string) {
	huma.Register(api, huma.Operation{
		OperationID: "health-live",
		Method:      http.MethodGet,
		Path:        "/api/v2/health/live",
		Summary:     "Liveness probe",
		Tags:        []string{"System"},
	}, func(_ context.Context, _ *struct{}) (*healthOutput, error) {
		return &healthOutput{Body: Health{
			Status:  "ok",
			Service: "heya-metadata",
			Version: version,
		}}, nil
	})
}

func registerReadiness(api huma.API, version string, checker ReadinessChecker) {
	huma.Register(api, huma.Operation{
		OperationID: "health-ready",
		Method:      http.MethodGet,
		Path:        "/api/v2/health/ready",
		Summary:     "Dependency-aware readiness probe",
		Tags:        []string{"System"},
	}, func(ctx context.Context, _ *struct{}) (*readinessOutput, error) {
		output := &readinessOutput{
			Status: http.StatusOK,
			Body: Readiness{
				Status:       "ok",
				Service:      "heya-metadata",
				Version:      version,
				Dependencies: map[string]DependencyHealth{},
			},
		}
		if checker == nil {
			return output, nil
		}
		for name, err := range checker.Check(ctx) {
			status := "ok"
			if err != nil {
				status = "unavailable"
				output.Status = http.StatusServiceUnavailable
				output.Body.Status = "not_ready"
				slog.Debug("readiness dependency unavailable", "dependency", name, "error", err)
			}
			output.Body.Dependencies[name] = DependencyHealth{Status: status}
		}
		return output, nil
	})
}

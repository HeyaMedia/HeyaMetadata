package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHealthEndpoints(t *testing.T) {
	t.Parallel()

	handler := New("v0.0.1-test").Handler()
	for _, path := range []string{"/api/v2/health/live", "/api/v2/health/ready"} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()

			request := httptest.NewRequest(http.MethodGet, path, nil)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)

			if response.Code != http.StatusOK {
				t.Fatalf("status: got %d, want %d", response.Code, http.StatusOK)
			}

			if path == "/api/v2/health/live" {
				var body Health
				if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
					t.Fatalf("decode response: %v", err)
				}
				if body.Status != "ok" || body.Service != "heya-metadata" || body.Version != "v0.0.1-test" {
					t.Fatalf("unexpected body: %+v", body)
				}
			}
		})
	}
}

type readinessCheckerFunc func(context.Context) map[string]error

func (function readinessCheckerFunc) Check(ctx context.Context) map[string]error {
	return function(ctx)
}

func TestReadinessReportsDependencyFailure(t *testing.T) {
	t.Parallel()

	checker := readinessCheckerFunc(func(context.Context) map[string]error {
		return map[string]error{"postgres": nil, "redis": fmt.Errorf("offline")}
	})
	request := httptest.NewRequest(http.MethodGet, "/api/v2/health/ready", nil)
	response := httptest.NewRecorder()
	NewWithReadiness("test", checker).Handler().ServeHTTP(response, request)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status: got %d, want %d", response.Code, http.StatusServiceUnavailable)
	}
	var body Readiness
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Status != "not_ready" || body.Dependencies["redis"].Status != "unavailable" {
		t.Fatalf("unexpected readiness body: %+v", body)
	}
}

func TestOpenAPIDocumentContainsPublicRoutes(t *testing.T) {
	t.Parallel()

	document, err := OpenAPIDocument("test", "json", "3.1")
	if err != nil {
		t.Fatalf("render OpenAPI: %v", err)
	}

	text := string(document)
	for _, path := range []string{
		"/api/v2/health/live", "/api/v2/health/ready",
		"/api/v2/entities/{id}", "/api/v2/resolutions", "/api/v2/jobs/{id}", "/api/v2/search", "/api/v2/changes",
		"/api/v2/images/{id}",
		"/api/v2/discoveries", "/api/v2/discoveries/{id}",
	} {
		if !strings.Contains(text, path) {
			t.Errorf("OpenAPI document does not contain %s", path)
		}
	}
	if !strings.Contains(text, "X-Heya-TMDB-API-Key") {
		t.Error("OpenAPI document does not expose request-scoped TMDB credentials")
	}
	if !strings.Contains(text, "X-Heya-OMDB-API-Key") {
		t.Error("OpenAPI document does not expose request-scoped OMDb credentials")
	}
	if !strings.Contains(text, "X-Heya-TVDB-API-Key") {
		t.Error("OpenAPI document does not expose request-scoped TVDB credentials")
	}
	if !strings.Contains(text, "X-Heya-Fanart-API-Key") {
		t.Error("OpenAPI document does not expose request-scoped Fanart.tv credentials")
	}
	for _, header := range []string{"X-Heya-Apple-API-Key", "X-Heya-Discogs-API-Key", "X-Heya-LastFM-API-Key"} {
		if !strings.Contains(text, header) {
			t.Errorf("OpenAPI document does not expose %s", header)
		}
	}
	for _, kind := range []string{"tv_show", "anime"} {
		if !strings.Contains(text, kind) {
			t.Errorf("OpenAPI document does not preserve distinct %s kind", kind)
		}
	}
}

func TestDocsUseScalar(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest(http.MethodGet, "/api/docs", nil)
	response := httptest.NewRecorder()
	New("test").Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d", response.Code, http.StatusOK)
	}
	body := response.Body.String()
	if !strings.Contains(body, "@scalar/api-reference") {
		t.Error("documentation page does not load Scalar")
	}
	if strings.Contains(body, "@stoplight/elements") {
		t.Error("documentation page still loads Stoplight Elements")
	}
}

func TestPreferredWaitIsBounded(t *testing.T) {
	t.Parallel()
	if got := preferredWait("respond-async, wait=3"); got != 3*time.Second {
		t.Fatalf("wait: got %s", got)
	}
	if got := preferredWait("wait=30"); got != 5*time.Second {
		t.Fatalf("bounded wait: got %s", got)
	}
	if got := preferredWait("wait=invalid"); got != 0 {
		t.Fatalf("invalid wait: got %s", got)
	}
}

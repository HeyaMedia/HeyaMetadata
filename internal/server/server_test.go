package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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

			var body Health
			if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if body.Status != "ok" || body.Service != "heya-metadata" || body.Version != "v0.0.1-test" {
				t.Fatalf("unexpected body: %+v", body)
			}
		})
	}
}

func TestOpenAPIDocumentContainsHealthRoutes(t *testing.T) {
	t.Parallel()

	document, err := OpenAPIDocument("test", "json", "3.1")
	if err != nil {
		t.Fatalf("render OpenAPI: %v", err)
	}

	text := string(document)
	for _, path := range []string{"/api/v2/health/live", "/api/v2/health/ready"} {
		if !strings.Contains(text, path) {
			t.Errorf("OpenAPI document does not contain %s", path)
		}
	}
}

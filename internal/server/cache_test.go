package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCacheControlFor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		path string
		want string
	}{
		{path: "/api/v2/health/live", want: "no-store"},
		{path: "/api/v2/auth/me", want: "no-store"},
		{path: "/api/v2/admin/jobs", want: "no-store"},
		{path: "/api/v2/jobs/42", want: "no-store"},
		{path: "/api/v2/discoveries/abc", want: "no-store"},
		{path: "/api/v2/tv/discoveries", want: "no-store"},
		{path: "/api/v2/fingerprint-matches/abc", want: "no-store"},
		{path: "/api/v2/changes", want: "no-store"},
		{path: "/api/v2/entities/abc/refreshes", want: "no-store"},
		{path: "/api/v2/latest", want: "public, max-age=60, stale-while-revalidate=600"},
		{path: "/api/v2/browse", want: "public, max-age=300, stale-while-revalidate=3600"},
		{path: "/api/v2/stats", want: "public, max-age=300, stale-while-revalidate=3600"},
		{path: "/api/v2/collections", want: "public, max-age=300, stale-while-revalidate=3600"},
		{path: "/api/v2/search", want: "public, max-age=3600, stale-while-revalidate=86400"},
		{path: "/api/v2/entities/abc", want: "public, max-age=0, s-maxage=300, stale-while-revalidate=3600"},
		{path: "/api/openapi.json", want: "public, max-age=300, stale-while-revalidate=3600"},
		{path: "/schemas/Entity.json", want: "public, max-age=300, stale-while-revalidate=3600"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.path, func(t *testing.T) {
			t.Parallel()
			if got := cacheControlFor(test.path); got != test.want {
				t.Fatalf("cacheControlFor(%q) = %q, want %q", test.path, got, test.want)
			}
		})
	}
}

func TestCacheHeadersAddsPolicyAndETag(t *testing.T) {
	t.Parallel()

	handler := cacheHeaders(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"results":[]}`))
	}))

	first := httptest.NewRecorder()
	handler.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/api/v2/latest", nil))
	if first.Code != http.StatusOK {
		t.Fatalf("status = %d", first.Code)
	}
	if got := first.Header().Get("Cache-Control"); got != "public, max-age=60, stale-while-revalidate=600" {
		t.Fatalf("Cache-Control = %q", got)
	}
	etag := first.Header().Get("ETag")
	if etag == "" {
		t.Fatal("missing ETag")
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v2/latest", nil)
	request.Header.Set("If-None-Match", etag)
	second := httptest.NewRecorder()
	handler.ServeHTTP(second, request)
	if second.Code != http.StatusNotModified || second.Body.Len() != 0 {
		t.Fatalf("conditional response status=%d body=%q", second.Code, second.Body.String())
	}
}

func TestCacheHeadersNeverCachesMutationsOrErrors(t *testing.T) {
	t.Parallel()

	handler := cacheHeaders(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodGet {
			writer.WriteHeader(http.StatusNotFound)
		}
	}))

	for _, request := range []*http.Request{
		httptest.NewRequest(http.MethodPost, "/api/v2/discoveries", nil),
		httptest.NewRequest(http.MethodGet, "/api/v2/entities/missing", nil),
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if got := response.Header().Get("Cache-Control"); got != "no-store" {
			t.Fatalf("%s %s Cache-Control = %q", request.Method, request.URL.Path, got)
		}
	}
}

func TestCacheHeadersLetsReadyImagesOverrideDefault(t *testing.T) {
	t.Parallel()

	handler := cacheHeaders(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Cache-Control", "public, max-age=604800")
		writer.WriteHeader(http.StatusOK)
	}))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v2/images/00000000-0000-0000-0000-000000000000", nil))
	if got := response.Header().Get("Cache-Control"); got != "public, max-age=604800" {
		t.Fatalf("Cache-Control = %q", got)
	}
}

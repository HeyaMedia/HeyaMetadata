package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWithWebUIServesAssetsAndFallsBackForBrowserRoutes(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "_nuxt"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("<main>observatory</main>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "_nuxt", "app.js"), []byte("export default true"), 0o644); err != nil {
		t.Fatal(err)
	}

	api := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusTeapot)
		_, _ = writer.Write([]byte("api:" + request.URL.Path))
	})
	handler, err := WithWebUI(api, root)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name      string
		path      string
		accept    string
		status    int
		contains  string
		cachePart string
	}{
		{name: "root", path: "/", accept: "text/html", status: http.StatusOK, contains: "observatory", cachePart: "no-cache"},
		{name: "browser route", path: "/artists/canonical-id", accept: "text/html,application/xhtml+xml", status: http.StatusOK, contains: "observatory", cachePart: "no-cache"},
		{name: "asset", path: "/_nuxt/app.js", accept: "*/*", status: http.StatusOK, contains: "export default", cachePart: "immutable"},
		{name: "API", path: "/api/v2/health/live", accept: "text/html", status: http.StatusTeapot, contains: "api:/api/v2/health/live"},
		{name: "schema", path: "/schemas/Health.json", accept: "text/html", status: http.StatusTeapot, contains: "api:/schemas/Health.json"},
		{name: "missing asset", path: "/missing.png", accept: "image/png", status: http.StatusTeapot, contains: "api:/missing.png"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, test.path, nil)
			request.Header.Set("Accept", test.accept)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.status || !strings.Contains(response.Body.String(), test.contains) {
				t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
			}
			if test.cachePart != "" && !strings.Contains(response.Header().Get("Cache-Control"), test.cachePart) {
				t.Fatalf("Cache-Control=%q", response.Header().Get("Cache-Control"))
			}
		})
	}
}

func TestWithWebUIRejectsIncompleteRoot(t *testing.T) {
	if _, err := WithWebUI(http.NotFoundHandler(), t.TempDir()); err == nil {
		t.Fatal("expected missing index.html to fail")
	}
}

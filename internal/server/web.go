package server

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
)

// WithWebUI serves a compiled single-page application alongside the API. API
// and schema paths always remain authoritative; browser navigation falls back
// to index.html only for requests that accept HTML. For those HTML documents the
// server injects per-route <head> metadata (title, description, canonical, Open
// Graph, Twitter Card, JSON-LD) so non-JS crawlers and social scrapers receive
// real metadata. It also serves an authoritative robots.txt and a DB-backed
// sitemap.xml. The SEO path is strictly read-only and best-effort: any failure
// serves the unmodified shell. runtime may be nil, which disables entity
// resolution (static-route metadata and robots.txt still apply).
func WithWebUI(api http.Handler, root string, runtime *platform.Runtime, siteURL string) (http.Handler, error) {
	if api == nil {
		return nil, fmt.Errorf("API handler is required")
	}
	root, err := filepath.Abs(strings.TrimSpace(root))
	if err != nil {
		return nil, fmt.Errorf("resolve web root: %w", err)
	}
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("stat web root: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("web root %s is not a directory", root)
	}
	indexPath := filepath.Join(root, "index.html")
	if info, err = os.Stat(indexPath); err != nil || info.IsDir() {
		return nil, fmt.Errorf("web root %s has no index.html", root)
	}
	indexModTime := info.ModTime()
	index, err := os.ReadFile(indexPath)
	if err != nil {
		return nil, fmt.Errorf("read web index: %w", err)
	}

	seo := &seoRenderer{runtime: runtime, siteURL: strings.TrimRight(siteURL, "/")}
	files := http.FileServer(http.Dir(root))
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if isAPIPath(request.URL.Path) || (request.Method != http.MethodGet && request.Method != http.MethodHead) {
			api.ServeHTTP(writer, request)
			return
		}

		switch request.URL.Path {
		case "/robots.txt":
			writeRobots(writer, seo.siteURL)
			return
		case "/sitemap.xml":
			seo.writeSitemap(request.Context(), writer)
			return
		}

		cleanPath := path.Clean("/" + request.URL.Path)
		relativePath := strings.TrimPrefix(cleanPath, "/")
		candidate := filepath.Join(root, filepath.FromSlash(relativePath))
		if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
			if strings.HasPrefix(cleanPath, "/_nuxt/") {
				writer.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			files.ServeHTTP(writer, request)
			return
		}

		if cleanPath == "/" || strings.Contains(request.Header.Get("Accept"), "text/html") {
			body := index
			modTime := indexModTime
			if injected, ok := seo.render(request.Context(), index, cleanPath); ok {
				body = injected
				// The injected variant differs per route, so drop Last-Modified
				// to avoid a stale conditional match against the shared shell.
				modTime = time.Time{}
			}
			writer.Header().Set("Cache-Control", "no-cache")
			writer.Header().Set("Content-Type", "text/html; charset=utf-8")
			http.ServeContent(writer, request, "index.html", modTime, bytes.NewReader(body))
			return
		}

		api.ServeHTTP(writer, request)
	}), nil
}

func isAPIPath(value string) bool {
	return value == "/api" || strings.HasPrefix(value, "/api/") || value == "/v1" || strings.HasPrefix(value, "/v1/") || value == "/schemas" || strings.HasPrefix(value, "/schemas/")
}

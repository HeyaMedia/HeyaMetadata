// Package devproxy provides the stable development front door used by
// `make dev` while the Go API and Nuxt dev server restart independently.
package devproxy

import (
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
)

// New returns a handler that routes API traffic to backend and all other
// traffic to frontend. ReverseProxy handles WebSocket upgrades, so Nuxt HMR
// remains connected through the stable public listener.
func New(backend, frontend *url.URL, logger *slog.Logger) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}

	api := newProxy("api", backend, logger)
	web := newProxy("web", frontend, logger)

	mux := http.NewServeMux()
	mux.Handle("/api", api)
	mux.Handle("/api/", api)
	// robots.txt and sitemap.xml are authored by the Go backend so dev matches
	// prod; everything else is the Nuxt dev server.
	mux.Handle("/robots.txt", api)
	mux.Handle("/sitemap.xml", api)
	mux.Handle("/", web)
	return mux
}

func newProxy(name string, target *url.URL, logger *slog.Logger) *httputil.ReverseProxy {
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.FlushInterval = -1
	proxy.ErrorHandler = func(response http.ResponseWriter, request *http.Request, err error) {
		logger.Debug("development upstream unavailable",
			"upstream", name,
			"target", target.String(),
			"path", request.URL.Path,
			"error", err,
		)
		response.Header().Set("Content-Type", "text/plain; charset=utf-8")
		response.Header().Set("Retry-After", "1")
		response.WriteHeader(http.StatusBadGateway)
		_, _ = response.Write([]byte(name + " development server is unavailable\n"))
	}
	return proxy
}

package server

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
)

const compressionThreshold = 1024

// cacheRecorder buffers successful API responses so cacheHeaders can attach a
// stable ETag before anything is written to the client. The API documents are
// bounded JSON payloads; image bytes bypass this recorder and keep the headers
// supplied by the image handler.
type cacheRecorder struct {
	http.ResponseWriter
	buffer      bytes.Buffer
	status      int
	wroteHeader bool
}

func (recorder *cacheRecorder) WriteHeader(status int) {
	if recorder.wroteHeader {
		return
	}
	recorder.status = status
	recorder.wroteHeader = true
}

func (recorder *cacheRecorder) Write(body []byte) (int, error) {
	if !recorder.wroteHeader {
		recorder.WriteHeader(http.StatusOK)
	}
	return recorder.buffer.Write(body)
}

// cacheControlFor mirrors the proven cache contract from the original
// heya.media frontend while keeping stateful V2 workflows uncached. Rules are
// ordered from most specific to least specific.
func cacheControlFor(path string) string {
	switch {
	case strings.HasPrefix(path, "/api/v2/health"):
		return "no-store"
	case strings.HasPrefix(path, "/api/v2/auth"):
		return "no-store"
	case strings.HasPrefix(path, "/api/v2/admin"):
		return "no-store"
	case strings.HasPrefix(path, "/api/v2/jobs"):
		return "no-store"
	case strings.Contains(path, "/discoveries"):
		return "no-store"
	case strings.HasPrefix(path, "/api/v2/fingerprint-matches"):
		return "no-store"
	case strings.HasPrefix(path, "/api/v2/changes"):
		return "no-store"
	case strings.Contains(path, "/refreshes"):
		return "no-store"
	case strings.HasPrefix(path, "/api/v2/latest"):
		return "public, max-age=60, stale-while-revalidate=600"
	case strings.HasPrefix(path, "/api/v2/browse"):
		return "public, max-age=300, stale-while-revalidate=3600"
	case strings.HasPrefix(path, "/api/v2/stats"):
		return "public, max-age=300, stale-while-revalidate=3600"
	case strings.HasPrefix(path, "/api/v2/collections"):
		return "public, max-age=300, stale-while-revalidate=3600"
	case strings.HasPrefix(path, "/api/v2/search"):
		return "public, max-age=3600, stale-while-revalidate=86400"
	case strings.HasPrefix(path, "/api/openapi"), strings.HasPrefix(path, "/api/docs"), strings.HasPrefix(path, "/schemas/"):
		return "public, max-age=300, stale-while-revalidate=3600"
	case strings.HasPrefix(path, "/api/v2/"):
		// Canonical documents can change immediately after a provider refresh.
		// Browsers must revalidate their ETag on navigation while a shared edge
		// may absorb repeated traffic for a short window.
		return "public, max-age=0, s-maxage=300, stale-while-revalidate=3600"
	default:
		return "no-store"
	}
}

// cacheHeaders makes origin headers authoritative for browsers and shared
// caches such as Cloudflare. Non-GET requests, non-success responses, and
// stateful workflows are never cached. Cacheable JSON and schema responses get
// a representation ETag, allowing clients and the edge to revalidate cheaply.
func cacheHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet {
			writer.Header().Set("Cache-Control", "no-store")
			next.ServeHTTP(writer, request)
			return
		}

		// Canonical image responses already carry content-aware cache headers and
		// checksums. Default to no-store so pending/error responses remain safe;
		// a ready image overwrites this header in its Huma output.
		if strings.HasPrefix(request.URL.Path, "/api/v2/images/") {
			writer.Header().Set("Cache-Control", "no-store")
			next.ServeHTTP(writer, request)
			return
		}

		recorder := &cacheRecorder{ResponseWriter: writer, status: http.StatusOK}
		next.ServeHTTP(recorder, request)

		body := recorder.buffer.Bytes()
		success := recorder.status >= http.StatusOK && recorder.status < http.StatusMultipleChoices
		if !success {
			writer.Header().Set("Cache-Control", "no-store")
		} else if writer.Header().Get("Cache-Control") == "" {
			writer.Header().Set("Cache-Control", cacheControlFor(request.URL.Path))
		}

		if success && compressibleJSON(writer.Header(), body) {
			appendVary(writer.Header(), "Accept-Encoding")
			if acceptsEncoding(request.Header.Get("Accept-Encoding"), "gzip") {
				var compressed bytes.Buffer
				encoder := gzip.NewWriter(&compressed)
				if _, err := encoder.Write(body); err == nil {
					if err := encoder.Close(); err == nil {
						body = compressed.Bytes()
						writer.Header().Set("Content-Encoding", "gzip")
						writer.Header().Del("Content-Length")
					}
				} else {
					_ = encoder.Close()
				}
			}
		}

		if success && len(body) > 0 && writer.Header().Get("ETag") == "" && writer.Header().Get("Cache-Control") != "no-store" {
			sum := sha256.Sum256(body)
			etag := `"` + hex.EncodeToString(sum[:8]) + `"`
			writer.Header().Set("ETag", etag)
			if etagMatches(request.Header.Get("If-None-Match"), etag) {
				writer.WriteHeader(http.StatusNotModified)
				return
			}
		}

		writer.WriteHeader(recorder.status)
		_, _ = writer.Write(body)
	})
}

func compressibleJSON(header http.Header, body []byte) bool {
	if len(body) < compressionThreshold || header.Get("Content-Encoding") != "" {
		return false
	}
	contentType := strings.ToLower(strings.TrimSpace(strings.Split(header.Get("Content-Type"), ";")[0]))
	return contentType == "application/json" || contentType == "application/problem+json" || strings.HasSuffix(contentType, "+json")
}

func acceptsEncoding(header, wanted string) bool {
	wildcard := false
	for _, item := range strings.Split(strings.ToLower(header), ",") {
		parts := strings.Split(item, ";")
		name := strings.TrimSpace(parts[0])
		quality := 1.0
		for _, parameter := range parts[1:] {
			key, value, ok := strings.Cut(strings.TrimSpace(parameter), "=")
			if ok && key == "q" {
				parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
				if err != nil {
					quality = 0
				} else {
					quality = parsed
				}
			}
		}
		if name == wanted {
			return quality > 0
		}
		if name == "*" {
			wildcard = quality > 0
		}
	}
	return wildcard
}

func appendVary(header http.Header, value string) {
	for _, existing := range strings.Split(header.Get("Vary"), ",") {
		if strings.EqualFold(strings.TrimSpace(existing), value) {
			return
		}
	}
	if current := header.Get("Vary"); current != "" {
		header.Set("Vary", current+", "+value)
	} else {
		header.Set("Vary", value)
	}
}

func etagMatches(header, etag string) bool {
	if header == "*" {
		return true
	}
	for _, candidate := range strings.Split(header, ",") {
		if strings.TrimSpace(candidate) == etag {
			return true
		}
	}
	return false
}

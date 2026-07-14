package devproxy

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestRoutesAPIAndWebTraffic(t *testing.T) {
	t.Parallel()

	backend := testUpstream(t, "api")
	frontend := testUpstream(t, "web")
	handler := New(mustURL(t, backend.URL), mustURL(t, frontend.URL), quietLogger())

	for _, test := range []struct {
		path string
		want string
	}{
		{path: "/api", want: "api:/api"},
		{path: "/api/v2/health/live", want: "api:/api/v2/health/live"},
		{path: "/v1/ip", want: "api:/v1/ip"},
		{path: "/v1/check", want: "api:/v1/check"},
		{path: "/", want: "web:/"},
		{path: "/_nuxt/client.js", want: "web:/_nuxt/client.js"},
	} {
		t.Run(test.path, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, test.path, nil)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)

			if response.Code != http.StatusOK {
				t.Fatalf("status: got %d, want %d", response.Code, http.StatusOK)
			}
			if body := strings.TrimSpace(response.Body.String()); body != test.want {
				t.Fatalf("body: got %q, want %q", body, test.want)
			}
		})
	}
}

func TestUnavailableUpstreamReturnsBadGateway(t *testing.T) {
	t.Parallel()

	frontend := testUpstream(t, "web")
	handler := New(mustURL(t, "http://127.0.0.1:1"), mustURL(t, frontend.URL), quietLogger())
	request := httptest.NewRequest(http.MethodGet, "/api/v2/health/live", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusBadGateway {
		t.Fatalf("status: got %d, want %d", response.Code, http.StatusBadGateway)
	}
	if response.Header().Get("Retry-After") != "1" {
		t.Error("missing Retry-After header")
	}
}

func testUpstream(t *testing.T, name string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		_, _ = io.WriteString(response, name+":"+request.URL.Path)
	}))
	t.Cleanup(server.Close)
	return server
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

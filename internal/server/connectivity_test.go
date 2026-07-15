package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"sync"
	"testing"

	"github.com/HeyaMedia/HeyaMetadata/internal/connectivity"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
)

type fakeConnectivityProber struct {
	mutex     sync.Mutex
	address   netip.Addr
	port      int
	challenge string
	started   chan struct{}
	finish    chan struct{}
}

type fakePublicIPMatcher netip.Addr

func (matcher fakePublicIPMatcher) Matches(address netip.Addr) bool {
	return netip.Addr(matcher) == address
}

func (prober *fakeConnectivityProber) Probe(_ context.Context, address netip.Addr, port int, challenge string) connectivity.Result {
	prober.mutex.Lock()
	prober.address = address
	prober.port = port
	prober.challenge = challenge
	prober.mutex.Unlock()
	if prober.started != nil {
		close(prober.started)
	}
	if prober.finish != nil {
		<-prober.finish
	}
	return connectivity.Result{ObservedIP: address.String(), Reachable: true, Verified: true, LatencyMS: 12}
}

func TestConnectivityIPUsesTrustedProxySource(t *testing.T) {
	t.Parallel()
	handler, _ := connectivityTestHandler(t, &fakeConnectivityProber{})
	request := httptest.NewRequest(http.MethodGet, "/v1/ip", nil)
	request.RemoteAddr = "10.0.0.8:12345"
	request.Header.Set("CF-Connecting-IP", "1.1.1.1")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var body connectivityIPResult
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.IP != "1.1.1.1" {
		t.Fatalf("unexpected body: %+v", body)
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control = %q", response.Header().Get("Cache-Control"))
	}
}

func TestConnectivityIPIgnoresUntrustedForwardingHeader(t *testing.T) {
	t.Parallel()
	handler, _ := connectivityTestHandler(t, &fakeConnectivityProber{})
	request := httptest.NewRequest(http.MethodGet, "/v1/ip", nil)
	request.RemoteAddr = "8.8.8.8:12345"
	request.Header.Set("CF-Connecting-IP", "1.1.1.1")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK || response.Body.String() != "{\"ip\":\"8.8.8.8\"}\n" {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestConnectivityRejectsInvalidInputAndPrivateSources(t *testing.T) {
	t.Parallel()
	handler, _ := connectivityTestHandler(t, &fakeConnectivityProber{})
	tests := []struct {
		name       string
		remoteAddr string
		body       string
		status     int
	}{
		{"privileged port", "8.8.8.8:12345", `{"port":443,"challenge":"0123456789abcdef"}`, http.StatusBadRequest},
		{"uppercase challenge", "8.8.8.8:12345", `{"port":47231,"challenge":"0123456789ABCDEf"}`, http.StatusBadRequest},
		{"missing challenge", "8.8.8.8:12345", `{"port":47231}`, http.StatusBadRequest},
		{"malformed JSON", "8.8.8.8:12345", `{"port":`, http.StatusUnprocessableEntity},
		{"private source", "127.0.0.1:12345", `{"port":47231,"challenge":"0123456789abcdef"}`, http.StatusBadRequest},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			request := httptest.NewRequest(http.MethodPost, "/v1/check", bytes.NewBufferString(test.body))
			request.Header.Set("Content-Type", "application/json")
			request.RemoteAddr = test.remoteAddr
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.status {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
}

func TestConnectivityCheckPassesOnlyObservedAddressToProber(t *testing.T) {
	t.Parallel()
	prober := &fakeConnectivityProber{}
	handler, _ := connectivityTestHandler(t, prober)
	request := httptest.NewRequest(http.MethodPost, "/v1/check", bytes.NewBufferString(`{"port":47231,"challenge":"0123456789abcdef"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("CF-Connecting-IP", "1.1.1.1")
	request.RemoteAddr = "10.0.0.8:12345"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var body connectivity.Result
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if !body.Reachable || !body.Verified || body.ObservedIP != "1.1.1.1" || body.Error != nil {
		t.Fatalf("unexpected result: %+v", body)
	}
	prober.mutex.Lock()
	defer prober.mutex.Unlock()
	if prober.address.String() != "1.1.1.1" || prober.port != 47231 || prober.challenge != "0123456789abcdef" {
		t.Fatalf("unexpected probe arguments: %s %d", prober.address, prober.port)
	}
}

func TestConnectivityCheckSkipsSameNetworkHairpinProbe(t *testing.T) {
	t.Parallel()
	prober := &fakeConnectivityProber{}
	resolver, err := connectivity.NewClientIPResolver(nil)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("connectivity-test", "test"))
	address := netip.MustParseAddr("8.8.8.8")
	registerConnectivityService(api, resolver, connectivity.NewLimiter(nil), prober, fakePublicIPMatcher(address))
	handler := captureRequestDetails(cacheHeaders(mux))

	response := performCheck(handler, "8.8.8.8:12345")
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var body connectivity.Result
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Reachable || body.Verified || body.ObservedIP != "8.8.8.8" || body.TLS != nil || body.Error == nil || body.Error.Code != "same_network" {
		t.Fatalf("unexpected result: %+v", body)
	}
	if body.Error.Detail != "the check service shares a public IP with the target — cannot probe from outside" {
		t.Fatalf("unexpected detail: %q", body.Error.Detail)
	}
	prober.mutex.Lock()
	defer prober.mutex.Unlock()
	if prober.address.IsValid() {
		t.Fatalf("hairpin probe unexpectedly dialed %s", prober.address)
	}
}

func TestConnectivityCheckReturnsExactInflightRateLimitBody(t *testing.T) {
	t.Parallel()
	prober := &fakeConnectivityProber{started: make(chan struct{}), finish: make(chan struct{})}
	handler, _ := connectivityTestHandler(t, prober)

	firstDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		firstDone <- performCheck(handler, "8.8.8.8:12345")
	}()
	<-prober.started

	second := performCheck(handler, "8.8.8.8:12346")
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("status=%d body=%s", second.Code, second.Body.String())
	}
	var body map[string]any
	if err := json.NewDecoder(second.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body) != 1 || body["retry_after_seconds"] == nil {
		t.Fatalf("unexpected body: %#v", body)
	}
	if second.Header().Get("Retry-After") == "" {
		t.Fatal("missing Retry-After header")
	}

	close(prober.finish)
	if first := <-firstDone; first.Code != http.StatusOK {
		t.Fatalf("first status=%d body=%s", first.Code, first.Body.String())
	}
}

func performCheck(handler http.Handler, remoteAddr string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/v1/check", bytes.NewBufferString(`{"port":47231,"challenge":"0123456789abcdef"}`))
	request.Header.Set("Content-Type", "application/json")
	request.RemoteAddr = remoteAddr
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func connectivityTestHandler(t *testing.T, prober connectivityProbeRunner) (http.Handler, huma.API) {
	t.Helper()
	resolver, err := connectivity.NewClientIPResolver([]string{"10.0.0.0/8"})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("connectivity-test", "test"))
	registerConnectivityService(api, resolver, connectivity.NewLimiter(nil), prober, nil)
	return captureRequestDetails(cacheHeaders(mux)), api
}

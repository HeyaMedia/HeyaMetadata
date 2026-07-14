package connectivity

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strconv"
	"strings"
	"testing"
)

const testChallenge = "0123456789abcdef0123456789abcdef"

func TestProbeVerifiesChallengeAndDescribesTLS(t *testing.T) {
	t.Parallel()
	server := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/connectivity/probe" {
			t.Errorf("path = %s", request.URL.Path)
		}
		if request.Host != "127.0.0.1" {
			t.Errorf("Host = %s", request.Host)
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"challenge":"` + testChallenge + `"}`))
	}))
	defer server.Close()

	result := NewProber().Probe(context.Background(), netip.MustParseAddr("127.0.0.1"), serverPort(t, server.Listener.Addr()), testChallenge)
	if !result.Reachable || !result.Verified || result.Error != nil {
		t.Fatalf("unexpected probe result: %+v", result)
	}
	if result.TLS == nil || result.TLS.LeafSHA256 == "" || !result.TLS.SelfSigned {
		t.Fatalf("missing TLS evidence: %+v", result.TLS)
	}
	if result.LatencyMS < 0 {
		t.Fatalf("negative latency: %d", result.LatencyMS)
	}
}

func TestProbeClassifiesHTTPAndChallengeFailures(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		handler  http.Handler
		wantCode string
	}{
		{
			name: "challenge mismatch",
			handler: http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				_, _ = response.Write([]byte(`{"challenge":"ffffffffffffffff"}`))
			}),
			wantCode: "challenge_mismatch",
		},
		{
			name: "non 200",
			handler: http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				response.WriteHeader(http.StatusNotFound)
			}),
			wantCode: "http_error",
		},
		{
			name: "invalid JSON",
			handler: http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				_, _ = response.Write([]byte(`not-json`))
			}),
			wantCode: "http_error",
		},
		{
			name: "oversized response",
			handler: http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				_, _ = response.Write([]byte(strings.Repeat("x", probeBodyLimit+1)))
			}),
			wantCode: "http_error",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewTLSServer(test.handler)
			defer server.Close()
			result := NewProber().Probe(context.Background(), netip.MustParseAddr("127.0.0.1"), serverPort(t, server.Listener.Addr()), testChallenge)
			if !result.Reachable || result.Verified || result.Error == nil || result.Error.Code != test.wantCode {
				t.Fatalf("unexpected result: %+v", result)
			}
			if result.TLS == nil {
				t.Fatal("TLS evidence should survive an HTTP-level failure")
			}
		})
	}
}

func TestProbeClassifiesTransportFailures(t *testing.T) {
	t.Parallel()
	t.Run("TLS handshake", func(t *testing.T) {
		t.Parallel()
		server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		defer server.Close()
		result := NewProber().Probe(context.Background(), netip.MustParseAddr("127.0.0.1"), serverPort(t, server.Listener.Addr()), testChallenge)
		if result.Reachable || result.TLS != nil || result.Error == nil || result.Error.Code != "tls_handshake" {
			t.Fatalf("unexpected result: %+v", result)
		}
	})

	t.Run("connection refused", func(t *testing.T) {
		t.Parallel()
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		port := serverPort(t, listener.Addr())
		_ = listener.Close()
		result := NewProber().Probe(context.Background(), netip.MustParseAddr("127.0.0.1"), port, testChallenge)
		if result.Reachable || result.Error == nil || result.Error.Code != "connection_refused" {
			t.Fatalf("unexpected result: %+v", result)
		}
	})

	t.Run("timeout", func(t *testing.T) {
		t.Parallel()
		prober := NewProber()
		prober.dialContext = func(context.Context, string, string) (net.Conn, error) {
			return nil, context.DeadlineExceeded
		}
		result := prober.Probe(context.Background(), netip.MustParseAddr("127.0.0.1"), 47231, testChallenge)
		if result.Reachable || result.Error == nil || result.Error.Code != "timeout" {
			t.Fatalf("unexpected result: %+v", result)
		}
	})
}

func TestProbeNeverUsesDNS(t *testing.T) {
	t.Parallel()
	prober := NewProber()
	var network, target string
	prober.dialContext = func(_ context.Context, gotNetwork, gotTarget string) (net.Conn, error) {
		network, target = gotNetwork, gotTarget
		return nil, errors.New("stop")
	}
	address := netip.MustParseAddr("2606:4700:4700::1111")
	_ = prober.Probe(context.Background(), address, 47231, testChallenge)
	if network != "tcp" || target != "[2606:4700:4700::1111]:47231" {
		t.Fatalf("dial = %s %s", network, target)
	}
}

func serverPort(t *testing.T, address net.Addr) int {
	t.Helper()
	_, rawPort, err := net.SplitHostPort(address.String())
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(rawPort)
	if err != nil {
		t.Fatal(err)
	}
	return port
}

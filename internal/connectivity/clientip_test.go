package connectivity

import (
	"net/http"
	"net/netip"
	"testing"
)

func TestClientIPResolverTrustsHeadersOnlyFromConfiguredPeers(t *testing.T) {
	t.Parallel()
	resolver, err := NewClientIPResolver([]string{"10.0.0.0/8", "127.0.0.0/8"})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name       string
		remoteAddr string
		headers    http.Header
		want       string
	}{
		{
			name:       "direct peer ignores spoofed Cloudflare header",
			remoteAddr: "8.8.8.8:443",
			headers:    http.Header{"Cf-Connecting-Ip": {"1.1.1.1"}},
			want:       "8.8.8.8",
		},
		{
			name:       "trusted peer accepts Cloudflare header",
			remoteAddr: "10.0.0.8:443",
			headers:    http.Header{"Cf-Connecting-Ip": {"1.1.1.1"}},
			want:       "1.1.1.1",
		},
		{
			name:       "rightmost untrusted forwarded hop wins",
			remoteAddr: "10.0.0.8:443",
			headers:    http.Header{"X-Forwarded-For": {"192.168.1.1, 8.8.4.4, 10.0.0.7"}},
			want:       "8.8.4.4",
		},
		{
			name:       "IPv4 mapped address is normalized",
			remoteAddr: "[::ffff:8.8.8.8]:443",
			want:       "8.8.8.8",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			request := &http.Request{RemoteAddr: test.remoteAddr, Header: test.headers}
			got, resolveErr := resolver.Resolve(request)
			if resolveErr != nil {
				t.Fatal(resolveErr)
			}
			if got.String() != test.want {
				t.Fatalf("resolved address = %s, want %s", got, test.want)
			}
		})
	}
}

func TestClientIPResolverRejectsNonPublicTargets(t *testing.T) {
	t.Parallel()
	resolver, err := NewClientIPResolver([]string{"10.0.0.0/8"})
	if err != nil {
		t.Fatal(err)
	}
	for _, address := range []string{
		"127.0.0.1", "10.1.2.3", "100.64.0.1", "169.254.1.1", "192.0.2.1",
		"192.88.99.1", "198.18.0.1", "198.51.100.1", "203.0.113.1", "224.0.0.1", "240.0.0.1",
		"::1", "64:ff9b::1", "64:ff9b:1::1", "fc00::1", "fe80::1", "2001:db8::1", "3fff::1", "5f00::1", "ff02::1",
	} {
		t.Run(address, func(t *testing.T) {
			t.Parallel()
			request := &http.Request{
				RemoteAddr: "10.0.0.8:443",
				Header:     http.Header{"Cf-Connecting-Ip": {address}},
			}
			if _, resolveErr := resolver.Resolve(request); resolveErr == nil {
				t.Fatal("expected target to be rejected")
			}
		})
	}
}

func TestIsPublicIP(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"1.1.1.1", "8.8.8.8", "2606:4700:4700::1111"} {
		if !IsPublicIP(netip.MustParseAddr(value)) {
			t.Errorf("%s should be public", value)
		}
	}
}

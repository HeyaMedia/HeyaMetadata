// Package connectivity implements the public outside-in connectivity check
// used by Heya media servers. Its resolver is intentionally strict: the only
// possible dial target is the public source address of the HTTP request.
package connectivity

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// ClientIPResolver accepts forwarding headers only from explicitly trusted
// immediate peers. Prefixes are normalized at construction time.
type ClientIPResolver struct {
	trusted []netip.Prefix
}

func NewClientIPResolver(cidrs []string) (*ClientIPResolver, error) {
	resolver := &ClientIPResolver{trusted: make([]netip.Prefix, 0, len(cidrs))}
	for _, raw := range cidrs {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(raw))
		if err != nil {
			return nil, fmt.Errorf("parse trusted proxy CIDR %q: %w", raw, err)
		}
		resolver.trusted = append(resolver.trusted, prefix.Masked())
	}
	return resolver, nil
}

// Resolve returns the public caller address or an error when the peer/header
// chain is malformed or resolves to an address that must never be dialed.
func (resolver *ClientIPResolver) Resolve(request *http.Request) (netip.Addr, error) {
	peer, err := parseRemoteAddr(request.RemoteAddr)
	if err != nil {
		return netip.Addr{}, err
	}

	resolved := peer
	if resolver.isTrusted(peer) {
		if raw := strings.TrimSpace(request.Header.Get("CF-Connecting-IP")); raw != "" {
			resolved, err = parseForwardedAddr(raw)
			if err != nil {
				return netip.Addr{}, fmt.Errorf("invalid CF-Connecting-IP: %w", err)
			}
		} else if raw := strings.TrimSpace(request.Header.Get("X-Forwarded-For")); raw != "" {
			resolved, err = resolver.resolveForwardedFor(raw, peer)
			if err != nil {
				return netip.Addr{}, err
			}
		}
	}

	resolved = resolved.Unmap()
	if !IsPublicIP(resolved) {
		return netip.Addr{}, fmt.Errorf("source IP %s is private or reserved", resolved)
	}
	return resolved, nil
}

func (resolver *ClientIPResolver) resolveForwardedFor(raw string, peer netip.Addr) (netip.Addr, error) {
	parts := strings.Split(raw, ",")
	chain := make([]netip.Addr, 0, len(parts)+1)
	for _, part := range parts {
		address, err := parseForwardedAddr(strings.TrimSpace(part))
		if err != nil {
			return netip.Addr{}, fmt.Errorf("invalid X-Forwarded-For: %w", err)
		}
		chain = append(chain, address)
	}
	chain = append(chain, peer)

	// Walk from the origin backwards and stop at the first untrusted hop. This
	// ignores any client-supplied values prepended to a valid trusted chain.
	for index := len(chain) - 1; index >= 0; index-- {
		if !resolver.isTrusted(chain[index]) {
			return chain[index], nil
		}
	}
	return chain[0], nil
}

func (resolver *ClientIPResolver) isTrusted(address netip.Addr) bool {
	address = address.Unmap()
	for _, prefix := range resolver.trusted {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

func parseRemoteAddr(value string) (netip.Addr, error) {
	host, _, err := net.SplitHostPort(strings.TrimSpace(value))
	if err != nil {
		host = strings.Trim(strings.TrimSpace(value), "[]")
	}
	address, parseErr := netip.ParseAddr(host)
	if parseErr != nil {
		return netip.Addr{}, fmt.Errorf("invalid request source address: %w", parseErr)
	}
	return address.Unmap(), nil
}

func parseForwardedAddr(value string) (netip.Addr, error) {
	if strings.ContainsAny(value, "\r\n") {
		return netip.Addr{}, fmt.Errorf("address contains control characters")
	}
	address, err := netip.ParseAddr(strings.Trim(value, "[]"))
	if err != nil {
		return netip.Addr{}, err
	}
	return address.Unmap(), nil
}

var reservedPrefixes = mustPrefixes(
	"0.0.0.0/8",
	"100.64.0.0/10",
	"192.0.0.0/24",
	"192.0.2.0/24",
	"198.18.0.0/15",
	"198.51.100.0/24",
	"203.0.113.0/24",
	"192.88.99.0/24",
	"240.0.0.0/4",
	"64:ff9b::/96",
	"64:ff9b:1::/48",
	"100::/64",
	"2001:db8::/32",
	"3fff::/20",
	"5f00::/16",
)

// IsPublicIP rejects local, private, documentation, benchmarking, carrier-NAT,
// multicast, and otherwise non-global targets. IPv4-mapped IPv6 is normalized
// before evaluation so it cannot bypass the IPv4 deny list.
func IsPublicIP(address netip.Addr) bool {
	if !address.IsValid() {
		return false
	}
	address = address.Unmap()
	if !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLoopback() ||
		address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast() ||
		address.IsMulticast() || address.IsUnspecified() {
		return false
	}
	for _, prefix := range reservedPrefixes {
		if prefix.Contains(address) {
			return false
		}
	}
	return true
}

func mustPrefixes(values ...string) []netip.Prefix {
	prefixes := make([]netip.Prefix, 0, len(values))
	for _, value := range values {
		prefixes = append(prefixes, netip.MustParsePrefix(value))
	}
	return prefixes
}

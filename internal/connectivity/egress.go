package connectivity

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"
)

const (
	publicIPEchoTimeout = 5 * time.Second
	publicIPEchoLimit   = 128
)

// PublicIPCache tracks the public address used by this service for outbound
// traffic. Connectivity checks use it to avoid same-router hairpin probes,
// which are not a valid outside-in reachability test.
type PublicIPCache struct {
	mutex    sync.RWMutex
	address  netip.Addr
	endpoint string
	client   *http.Client
}

func NewPublicIPCache(endpoint string, client *http.Client) *PublicIPCache {
	if client == nil {
		client = &http.Client{
			Timeout: publicIPEchoTimeout,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	}
	return &PublicIPCache{endpoint: strings.TrimSpace(endpoint), client: client}
}

// Refresh obtains one literal public address from the configured echo
// service. A failed refresh leaves the last successful address intact.
func (cache *PublicIPCache) Refresh(ctx context.Context) error {
	if cache == nil || cache.endpoint == "" {
		return fmt.Errorf("public IP echo URL is not configured")
	}
	ctx, cancel := context.WithTimeout(ctx, publicIPEchoTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, cache.endpoint, nil)
	if err != nil {
		return fmt.Errorf("create public IP echo request: %w", err)
	}
	request.Header.Set("Accept", "text/plain")
	request.Header.Set("User-Agent", "HeyaMetadata/connectivity-check")
	response, err := cache.client.Do(request)
	if err != nil {
		return fmt.Errorf("fetch public egress IP: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("fetch public egress IP: echo returned HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, publicIPEchoLimit+1))
	if err != nil {
		return fmt.Errorf("read public egress IP: %w", err)
	}
	if len(body) > publicIPEchoLimit {
		return fmt.Errorf("read public egress IP: response exceeds %d bytes", publicIPEchoLimit)
	}
	address, err := netip.ParseAddr(strings.TrimSpace(string(body)))
	if err != nil {
		return fmt.Errorf("parse public egress IP: %w", err)
	}
	address = address.Unmap()
	if !IsPublicIP(address) {
		return fmt.Errorf("parse public egress IP: address %s is private or reserved", address)
	}
	cache.mutex.Lock()
	cache.address = address
	cache.mutex.Unlock()
	return nil
}

// Run refreshes the cached address until the process context is cancelled.
// The caller performs the initial boot-time refresh synchronously.
func (cache *PublicIPCache) Run(ctx context.Context, interval time.Duration, onError func(error)) {
	if cache == nil || interval <= 0 {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := cache.Refresh(ctx); err != nil && onError != nil {
				onError(err)
			}
		}
	}
}

func (cache *PublicIPCache) Matches(address netip.Addr) bool {
	if cache == nil {
		return false
	}
	cache.mutex.RLock()
	publicAddress := cache.address
	cache.mutex.RUnlock()
	return publicAddress.IsValid() && publicAddress == address.Unmap()
}

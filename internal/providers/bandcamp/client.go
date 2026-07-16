// Package bandcamp collects artist metadata from public Bandcamp band pages.
//
// Bandcamp has no general-purpose API: the official one is gated to
// label/merch partners. Band pages are served to unauthenticated clients and
// embed a machine-readable data-band JSON attribute, which is what this
// collector reads. Rate limits are undocumented, so the default gate is
// deliberately conservative.
package bandcamp

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/config"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers"
)

var subdomainPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

type Client struct {
	config config.BandcampConfig
	http   *providers.HTTPClient
	gate   *providers.RequestGate
}

func New(config config.BandcampConfig) *Client {
	return newClient(config, providers.NewHTTPClient(30*time.Second))
}

func NewCached(config config.BandcampConfig, resolver providers.PayloadResolver) *Client {
	return newClient(config, providers.NewCachedHTTPClient(30*time.Second, resolver))
}

func newClient(config config.BandcampConfig, client *providers.HTTPClient) *Client {
	return &Client{
		config: config,
		http:   client,
		gate:   providers.SharedRequestGate("bandcamp:"+strings.TrimRight(config.BaseURL, "/"), config.RequestsPerSecond),
	}
}

func (c *Client) Capability() providers.Capability {
	return providers.Capability{
		Provider: "bandcamp", EntityKind: "music_source",
		RawRetention:        providers.RetentionPolicy{Class: "provider_raw_48h", Duration: 48 * time.Hour, ObjectPrefix: "ephemeral/48h"},
		ResponseCache:       providers.ResponseCachePolicy{ReuseDuration: 48 * time.Hour, NegativeDuration: 24 * time.Hour, RedisBodyDuration: time.Hour, MaxRedisBodyBytes: 4 * 1024 * 1024},
		AcceptedIdentifiers: []providers.Identifier{{Provider: "bandcamp", Namespace: "artist"}, {Provider: "bandcamp", Namespace: "album"}},
		Provides:            []providers.Scope{providers.ScopeIdentity, providers.ScopeTitles, providers.ScopeReleases, providers.ScopeArtwork},
	}
}

func (c *Client) Collect(ctx context.Context, identifier providers.Identifier) ([]providers.Payload, error) {
	if identifier.Provider != "bandcamp" {
		return nil, fmt.Errorf("Bandcamp collector requires a Bandcamp identifier")
	}
	value := strings.ToLower(strings.TrimSpace(identifier.Value))
	var subdomain, path string
	switch identifier.Namespace {
	case "artist":
		subdomain = value
	case "album":
		parts := strings.SplitN(value, "/", 2)
		if len(parts) != 2 || !subdomainPattern.MatchString(parts[1]) {
			return nil, fmt.Errorf("Bandcamp album collector requires a subdomain/slug identifier")
		}
		subdomain, path = parts[0], "/album/"+parts[1]
	default:
		return nil, fmt.Errorf("Bandcamp collector requires an artist or album identifier")
	}
	if !subdomainPattern.MatchString(subdomain) {
		return nil, fmt.Errorf("Bandcamp collector requires a valid artist subdomain")
	}
	pageURL := strings.ReplaceAll(c.config.PageURLTemplate, "{subdomain}", subdomain) + path
	request, err := http.NewRequest(http.MethodGet, pageURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build Bandcamp request: %w", err)
	}
	request.Header.Set("Accept", "text/html")
	payload, err := c.http.DoClassified(ctx, request, providers.Payload{
		Provider: "bandcamp", ProviderNamespace: identifier.Namespace, ProviderRecordID: value,
		RequestKey: identifier.Namespace + "/" + value,
	}, func() error {
		return c.gate.Wait(ctx)
	}, classify)
	if err != nil {
		return nil, err
	}
	return []providers.Payload{payload}, nil
}

var dataBandAttributePattern = regexp.MustCompile(`data-band="[^"]+"`)

// classify marks pages without a data-band attribute as non-reusable: either
// Bandcamp changed its markup or it served an interstitial, and neither
// deserves a week in the shared cache. Missing bands surface as HTTP 404 and
// follow the negative-cache policy.
func classify(payload *providers.Payload) {
	if payload.StatusCode != http.StatusOK {
		return
	}
	if !dataBandAttributePattern.Match(payload.Body) {
		zero := time.Duration(0)
		payload.ReuseDurationOverride = &zero
	}
}

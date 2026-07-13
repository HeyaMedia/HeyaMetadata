package kitsu

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/config"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers"
)

type Client struct {
	config config.KitsuConfig
	http   *providers.HTTPClient
	gate   *providers.RequestGate
}

func New(cfg config.KitsuConfig) *Client {
	return newClient(cfg, providers.NewHTTPClient(30*time.Second))
}
func NewCached(cfg config.KitsuConfig, resolver providers.PayloadResolver) *Client {
	return newClient(cfg, providers.NewCachedHTTPClient(30*time.Second, resolver))
}
func newClient(cfg config.KitsuConfig, client *providers.HTTPClient) *Client {
	return &Client{config: cfg, http: client, gate: providers.SharedRequestGate("kitsu:"+strings.TrimRight(cfg.BaseURL, "/"), cfg.RequestsPerSecond)}
}
func (c *Client) Capability() providers.Capability {
	return providers.Capability{Provider: "kitsu", EntityKind: "manga_source", RawRetention: providers.RetentionPolicy{Class: "provider_raw_48h", Duration: 48 * time.Hour, ObjectPrefix: "ephemeral/48h"}, ResponseCache: providers.ResponseCachePolicy{ReuseDuration: 12 * time.Hour, NegativeDuration: time.Hour, RedisBodyDuration: time.Hour, MaxRedisBodyBytes: 8 << 20}, AcceptedIdentifiers: []providers.Identifier{{Provider: "kitsu", Namespace: "manga"}}, Provides: []providers.Scope{providers.ScopeIdentity, providers.ScopeTitles, providers.ScopeDescriptions, providers.ScopeClassification, providers.ScopeReleases, providers.ScopeRatings, providers.ScopeArtwork}}
}
func (c *Client) Search(ctx context.Context, query string, limit int) (providers.Payload, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return providers.Payload{}, fmt.Errorf("Kitsu search query is required")
	}
	if limit < 1 || limit > 20 {
		limit = 10
	}
	return c.get(ctx, "/manga", url.Values{"filter[text]": {query}, "page[limit]": {strconv.Itoa(limit)}}, providers.Payload{Provider: "kitsu", ProviderNamespace: "manga_search", ProviderRecordID: query}, 6*time.Hour)
}
func (c *Client) Collect(ctx context.Context, id providers.Identifier) ([]providers.Payload, error) {
	value := strings.TrimSpace(id.Value)
	if id.Provider != "kitsu" || id.Namespace != "manga" || value == "" {
		return nil, fmt.Errorf("Kitsu collector requires kitsu:manga identifier")
	}
	if _, err := strconv.ParseInt(value, 10, 64); err != nil {
		return nil, fmt.Errorf("invalid Kitsu manga ID")
	}
	detail, err := c.get(ctx, "/manga/"+value, nil, providers.Payload{Provider: "kitsu", ProviderNamespace: "manga", ProviderRecordID: value}, 12*time.Hour)
	if err != nil {
		return nil, err
	}
	mappings, err := c.get(ctx, "/manga/"+value+"/mappings", nil, providers.Payload{Provider: "kitsu", ProviderNamespace: "manga_mappings", ProviderRecordID: value}, 24*time.Hour)
	if err != nil {
		return []providers.Payload{detail}, nil
	}
	return []providers.Payload{detail, mappings}, nil
}
func (c *Client) get(ctx context.Context, path string, values url.Values, payload providers.Payload, reuse time.Duration) (providers.Payload, error) {
	u, err := url.Parse(strings.TrimRight(c.config.BaseURL, "/") + path)
	if err != nil {
		return providers.Payload{}, err
	}
	if values != nil {
		u.RawQuery = values.Encode()
	}
	payload.RequestKey = strings.TrimPrefix(path, "/")
	if u.RawQuery != "" {
		payload.RequestKey += "?" + u.RawQuery
	}
	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return providers.Payload{}, err
	}
	req.Header.Set("Accept", "application/vnd.api+json")
	return c.http.DoPrepared(ctx, req, payload, func(*http.Request) error { return c.gate.Wait(ctx) }, func(p *providers.Payload) {
		if p.StatusCode == http.StatusOK {
			var v any
			if json.Unmarshal(p.Body, &v) == nil {
				p.ReuseDurationOverride = &reuse
			}
		}
	})
}

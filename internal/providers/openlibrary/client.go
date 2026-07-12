package openlibrary

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/config"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers"
)

var keyPattern = regexp.MustCompile(`^OL[0-9]+[WMA]$`)

type Client struct {
	config config.OpenLibraryConfig
	http   *providers.HTTPClient
	gate   *providers.RequestGate
}

func New(config config.OpenLibraryConfig) *Client {
	return newClient(config, providers.NewHTTPClient(30*time.Second))
}
func NewCached(config config.OpenLibraryConfig, resolver providers.PayloadResolver) *Client {
	return newClient(config, providers.NewCachedHTTPClient(30*time.Second, resolver))
}
func newClient(config config.OpenLibraryConfig, httpClient *providers.HTTPClient) *Client {
	return &Client{config: config, http: httpClient, gate: providers.SharedRequestGate("openlibrary:"+strings.TrimRight(config.BaseURL, "/"), config.RequestsPerSecond)}
}
func (c *Client) Capability() providers.Capability {
	return providers.Capability{Provider: "openlibrary", EntityKind: "book_source", RawRetention: providers.RetentionPolicy{Class: "provider_raw_48h", Duration: 48 * time.Hour, ObjectPrefix: "ephemeral/48h"}, ResponseCache: providers.ResponseCachePolicy{ReuseDuration: 24 * time.Hour, NegativeDuration: time.Hour, RedisBodyDuration: time.Hour, MaxRedisBodyBytes: 8 << 20}, AcceptedIdentifiers: []providers.Identifier{{Provider: "openlibrary", Namespace: "work"}, {Provider: "openlibrary", Namespace: "edition"}, {Provider: "openlibrary", Namespace: "author"}}, Provides: []providers.Scope{providers.ScopeIdentity, providers.ScopeTitles, providers.ScopeDescriptions, providers.ScopeClassification, providers.ScopeReleases, providers.ScopeRatings, providers.ScopeCredits, providers.ScopeArtwork}}
}
func (c *Client) Search(ctx context.Context, query string, limit int) (providers.Payload, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return providers.Payload{}, fmt.Errorf("Open Library search query is required")
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	values := url.Values{"q": {query}, "limit": {strconv.Itoa(limit)}, "fields": {"key,title,subtitle,author_key,author_name,first_publish_year,edition_key,edition_count,isbn,language,subject,cover_i,ratings_average,ratings_count"}}
	return c.get(ctx, "/search.json", values, providers.Payload{Provider: "openlibrary", ProviderNamespace: "work_search", ProviderRecordID: query}, 6*time.Hour)
}
func (c *Client) Collect(ctx context.Context, id providers.Identifier) ([]providers.Payload, error) {
	value := strings.ToUpper(strings.TrimSpace(id.Value))
	if id.Provider != "openlibrary" || !keyPattern.MatchString(value) {
		return nil, fmt.Errorf("Open Library collector requires a valid Open Library key")
	}
	suffix := map[string]string{"work": "W", "edition": "M", "author": "A"}[id.Namespace]
	if suffix == "" || !strings.HasSuffix(value, suffix) {
		return nil, fmt.Errorf("Open Library key does not match namespace %q", id.Namespace)
	}
	path := "/" + id.Namespace + "s/" + value + ".json"
	p, err := c.get(ctx, path, nil, providers.Payload{Provider: "openlibrary", ProviderNamespace: id.Namespace, ProviderRecordID: value}, 24*time.Hour)
	if err != nil {
		return nil, err
	}
	return []providers.Payload{p}, nil
}
func (c *Client) Editions(ctx context.Context, work string, limit int) (providers.Payload, error) {
	work = strings.ToUpper(strings.TrimSpace(work))
	if !keyPattern.MatchString(work) || !strings.HasSuffix(work, "W") {
		return providers.Payload{}, fmt.Errorf("invalid Open Library work key")
	}
	if limit < 1 || limit > 100 {
		limit = 50
	}
	return c.get(ctx, "/works/"+work+"/editions.json", url.Values{"limit": {strconv.Itoa(limit)}}, providers.Payload{Provider: "openlibrary", ProviderNamespace: "work_editions", ProviderRecordID: work}, 12*time.Hour)
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
	req.Header.Set("Accept", "application/json")
	return c.http.DoPrepared(ctx, req, payload, func(r *http.Request) error {
		if strings.TrimSpace(c.config.UserAgent) == "" {
			return fmt.Errorf("Open Library requires an identified User-Agent")
		}
		if err := c.gate.Wait(ctx); err != nil {
			return err
		}
		r.Header.Set("User-Agent", c.config.UserAgent)
		return nil
	}, func(p *providers.Payload) {
		if p.StatusCode == http.StatusOK {
			var v any
			if json.Unmarshal(p.Body, &v) != nil {
				zero := time.Duration(0)
				p.ReuseDurationOverride = &zero
			} else {
				p.ReuseDurationOverride = &reuse
			}
		}
	})
}

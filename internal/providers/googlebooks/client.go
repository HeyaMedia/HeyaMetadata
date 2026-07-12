package googlebooks

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/config"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers"
)

type Client struct {
	config config.GoogleBooksConfig
	http   *providers.HTTPClient
	gate   *providers.RequestGate
	apiKey string
}

func New(config config.GoogleBooksConfig) *Client {
	return newClient(config, providers.NewHTTPClient(30*time.Second), "")
}
func NewCached(config config.GoogleBooksConfig, resolver providers.PayloadResolver, apiKey string) *Client {
	return newClient(config, providers.NewCachedHTTPClient(30*time.Second, resolver), apiKey)
}
func newClient(config config.GoogleBooksConfig, client *providers.HTTPClient, key string) *Client {
	return &Client{config: config, http: client, gate: providers.SharedRequestGate("googlebooks:"+strings.TrimRight(config.BaseURL, "/"), config.RequestsPerSecond), apiKey: key}
}
func (c *Client) Capability() providers.Capability {
	return providers.Capability{
		Provider: "googlebooks", EntityKind: "book_source",
		RawRetention:        providers.RetentionPolicy{Class: "provider_raw_48h", Duration: 48 * time.Hour, ObjectPrefix: "ephemeral/48h"},
		ResponseCache:       providers.ResponseCachePolicy{ReuseDuration: 24 * time.Hour, NegativeDuration: time.Hour, RedisBodyDuration: time.Hour, MaxRedisBodyBytes: 8 << 20},
		AcceptedIdentifiers: []providers.Identifier{{Provider: "isbn", Namespace: "isbn10"}, {Provider: "isbn", Namespace: "isbn13"}, {Provider: "googlebooks", Namespace: "volume"}},
		Provides:            []providers.Scope{providers.ScopeIdentity, providers.ScopeTitles, providers.ScopeDescriptions, providers.ScopeClassification, providers.ScopeReleases, providers.ScopeRatings, providers.ScopeCredits, providers.ScopeArtwork},
	}
}
func (c *Client) Collect(ctx context.Context, id providers.Identifier) ([]providers.Payload, error) {
	value := strings.TrimSpace(id.Value)
	if id.Provider == "googlebooks" && id.Namespace == "volume" && value != "" {
		payload, err := c.get(ctx, "/volumes/"+url.PathEscape(value), nil, providers.Payload{Provider: "googlebooks", ProviderNamespace: "volume", ProviderRecordID: value})
		return []providers.Payload{payload}, err
	}
	if id.Provider != "isbn" || (id.Namespace != "isbn10" && id.Namespace != "isbn13") {
		return nil, fmt.Errorf("Google Books requires ISBN or volume ID")
	}
	payload, err := c.get(ctx, "/volumes", url.Values{"q": {"isbn:" + strings.ReplaceAll(value, "-", "")}, "maxResults": {"10"}}, providers.Payload{Provider: "googlebooks", ProviderNamespace: "isbn_lookup", ProviderRecordID: value})
	return []providers.Payload{payload}, err
}
func (c *Client) get(ctx context.Context, path string, values url.Values, payload providers.Payload) (providers.Payload, error) {
	u, err := url.Parse(strings.TrimRight(c.config.BaseURL, "/") + path)
	if err != nil {
		return providers.Payload{}, err
	}
	if values == nil {
		values = url.Values{}
	}
	safe := url.Values{}
	for key, entries := range values {
		safe[key] = append([]string(nil), entries...)
	}
	key := c.apiKey
	if key == "" {
		key = c.config.APIKey
	}
	if key != "" {
		values.Set("key", key)
	}
	u.RawQuery = values.Encode()
	payload.RequestKey = strings.TrimPrefix(path, "/")
	if safe.Encode() != "" {
		payload.RequestKey += "?" + safe.Encode()
	}
	request, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return providers.Payload{}, err
	}
	request.Header.Set("Accept", "application/json")
	return c.http.DoPrepared(ctx, request, payload, func(*http.Request) error { return c.gate.Wait(ctx) }, func(payload *providers.Payload) {
		if payload.StatusCode == http.StatusOK {
			var value any
			if json.Unmarshal(payload.Body, &value) != nil {
				zero := time.Duration(0)
				payload.ReuseDurationOverride = &zero
			}
		}
	})
}

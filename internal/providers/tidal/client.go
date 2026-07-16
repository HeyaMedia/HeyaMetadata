package tidal

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
	config config.TidalConfig
	http   *providers.HTTPClient
	gate   *providers.RequestGate
}

func New(config config.TidalConfig) *Client {
	return newClient(config, providers.NewHTTPClient(30*time.Second))
}

func NewCached(config config.TidalConfig, resolver providers.PayloadResolver) *Client {
	return newClient(config, providers.NewCachedHTTPClient(30*time.Second, resolver))
}

func newClient(config config.TidalConfig, client *providers.HTTPClient) *Client {
	return &Client{
		config: config,
		http:   client,
		gate:   providers.SharedRequestGate("tidal:"+strings.TrimRight(config.BaseURL, "/"), config.RequestsPerSecond),
	}
}

func (c *Client) Capability() providers.Capability {
	return providers.Capability{
		Provider: "tidal", EntityKind: "music_source",
		RawRetention:        providers.RetentionPolicy{Class: "provider_raw_48h", Duration: 48 * time.Hour, ObjectPrefix: "ephemeral/48h"},
		ResponseCache:       providers.ResponseCachePolicy{ReuseDuration: 24 * time.Hour, NegativeDuration: time.Hour, RedisBodyDuration: time.Hour, MaxRedisBodyBytes: 4 * 1024 * 1024},
		AcceptedIdentifiers: []providers.Identifier{{Provider: "tidal", Namespace: "artist"}},
		Provides:            []providers.Scope{providers.ScopeIdentity, providers.ScopeRatings, providers.ScopeArtwork},
	}
}

func (c *Client) Collect(ctx context.Context, identifier providers.Identifier) ([]providers.Payload, error) {
	if identifier.Provider != "tidal" || identifier.Namespace != "artist" {
		return nil, fmt.Errorf("Tidal collector requires a Tidal artist ID")
	}
	id, err := strconv.ParseInt(strings.TrimSpace(identifier.Value), 10, 64)
	if err != nil || id < 1 {
		return nil, fmt.Errorf("Tidal collector requires a positive artist ID")
	}
	value := strconv.FormatInt(id, 10)
	payload, err := c.get(ctx, "/artists/"+value, url.Values{"countryCode": {c.country()}, "include": {"profileArt,similarArtists"}}, providers.Payload{
		Provider: "tidal", ProviderNamespace: "artist", ProviderRecordID: value,
	})
	if err != nil {
		return nil, err
	}
	return []providers.Payload{payload}, nil
}

func (c *Client) get(ctx context.Context, path string, values url.Values, payload providers.Payload) (providers.Payload, error) {
	requestURL, err := url.Parse(strings.TrimRight(c.config.BaseURL, "/") + path)
	if err != nil {
		return providers.Payload{}, fmt.Errorf("build Tidal URL: %w", err)
	}
	requestURL.RawQuery = values.Encode()
	payload.RequestKey = strings.TrimPrefix(path, "/") + "?" + values.Encode()
	request, err := http.NewRequest(http.MethodGet, requestURL.String(), nil)
	if err != nil {
		return providers.Payload{}, fmt.Errorf("build Tidal request: %w", err)
	}
	request.Header.Set("Accept", "application/vnd.api+json")
	return c.http.DoPrepared(ctx, request, payload, func(request *http.Request) error {
		if strings.TrimSpace(c.config.ClientID) == "" || strings.TrimSpace(c.config.ClientSecret) == "" {
			return fmt.Errorf("Tidal requires HEYA_METADATA_TIDAL_CLIENT_ID and HEYA_METADATA_TIDAL_CLIENT_SECRET")
		}
		if err := c.gate.Wait(ctx); err != nil {
			return err
		}
		token, tokenErr := accessToken(ctx, c.config.AuthURL, c.config.ClientID, c.config.ClientSecret)
		if tokenErr != nil {
			return tokenErr
		}
		request.Header.Set("Authorization", "Bearer "+token)
		return nil
	}, classify)
}

func (c *Client) country() string {
	if country := strings.ToUpper(strings.TrimSpace(c.config.Country)); len(country) == 2 {
		return country
	}
	return "US"
}

// classify marks malformed JSON:API bodies as non-reusable; JSON:API errors
// arrive with real HTTP error statuses and follow the negative-cache policy.
func classify(payload *providers.Payload) {
	if payload.StatusCode != http.StatusOK {
		return
	}
	var response struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(payload.Body, &response); err != nil || len(response.Data) == 0 {
		zero := time.Duration(0)
		payload.ReuseDurationOverride = &zero
	}
}

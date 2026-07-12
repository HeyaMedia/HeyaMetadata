package deezer

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

var namespaces = map[string]string{"artist": "artist", "album": "album", "track": "track"}

type Client struct {
	config config.DeezerConfig
	http   *providers.HTTPClient
	gate   *providers.RequestGate
}

func New(config config.DeezerConfig) *Client {
	return newClient(config, providers.NewHTTPClient(30*time.Second))
}
func NewCached(config config.DeezerConfig, resolver providers.PayloadResolver) *Client {
	return newClient(config, providers.NewCachedHTTPClient(30*time.Second, resolver))
}
func newClient(config config.DeezerConfig, client *providers.HTTPClient) *Client {
	return &Client{config: config, http: client, gate: providers.SharedRequestGate("deezer:"+strings.TrimRight(config.BaseURL, "/"), config.RequestsPerSecond)}
}

func (c *Client) Capability() providers.Capability {
	return providers.Capability{
		Provider: "deezer", EntityKind: "music_source",
		RawRetention:        providers.RetentionPolicy{Class: "provider_raw_48h", Duration: 48 * time.Hour, ObjectPrefix: "ephemeral/48h"},
		ResponseCache:       providers.ResponseCachePolicy{ReuseDuration: 12 * time.Hour, NegativeDuration: time.Hour, RedisBodyDuration: time.Hour, MaxRedisBodyBytes: 4 * 1024 * 1024},
		AcceptedIdentifiers: []providers.Identifier{{Provider: "deezer", Namespace: "artist"}, {Provider: "deezer", Namespace: "album"}, {Provider: "deezer", Namespace: "track"}},
		Provides: []providers.Scope{
			providers.ScopeIdentity, providers.ScopeTitles, providers.ScopeClassification,
			providers.ScopeReleases, providers.ScopeCredits, providers.ScopeArtwork,
		},
	}
}

func (c *Client) Collect(ctx context.Context, identifier providers.Identifier) ([]providers.Payload, error) {
	if identifier.Provider != "deezer" || namespaces[identifier.Namespace] == "" {
		return nil, fmt.Errorf("Deezer collector requires a deezer artist, album, or track ID")
	}
	id, err := strconv.ParseInt(identifier.Value, 10, 64)
	if err != nil || id < 1 {
		return nil, fmt.Errorf("Deezer %s collector requires a positive numeric ID", identifier.Namespace)
	}
	payload, err := c.get(ctx, "/"+identifier.Namespace+"/"+identifier.Value, nil,
		providers.Payload{Provider: "deezer", ProviderNamespace: identifier.Namespace, ProviderRecordID: identifier.Value},
		12*time.Hour,
	)
	if err != nil {
		return nil, err
	}
	return []providers.Payload{payload}, nil
}

func (c *Client) Search(ctx context.Context, namespace, query string, limit, index int) (providers.Payload, error) {
	if namespaces[namespace] == "" || strings.TrimSpace(query) == "" {
		return providers.Payload{}, fmt.Errorf("Deezer search requires artist, album, or track and a query")
	}
	if limit < 1 || limit > 200 {
		limit = 25
	}
	if index < 0 {
		index = 0
	}
	values := url.Values{"q": {strings.TrimSpace(query)}, "limit": {strconv.Itoa(limit)}, "index": {strconv.Itoa(index)}}
	return c.get(ctx, "/search/"+namespace, values,
		providers.Payload{Provider: "deezer", ProviderNamespace: namespace + "_search", ProviderRecordID: strings.TrimSpace(query)},
		6*time.Hour,
	)
}

func (c *Client) ArtistAlbums(ctx context.Context, artistID string, limit, index int) (providers.Payload, error) {
	id, err := strconv.ParseInt(artistID, 10, 64)
	if err != nil || id < 1 {
		return providers.Payload{}, fmt.Errorf("Deezer artist albums requires a positive artist ID")
	}
	if limit < 1 || limit > 200 {
		limit = 200
	}
	if index < 0 {
		index = 0
	}
	values := url.Values{"limit": {strconv.Itoa(limit)}, "index": {strconv.Itoa(index)}}
	return c.get(ctx, "/artist/"+artistID+"/albums", values, providers.Payload{Provider: "deezer", ProviderNamespace: "artist_albums", ProviderRecordID: artistID}, 6*time.Hour)
}
func (c *Client) LookupAlbumByUPC(ctx context.Context, upc string) (providers.Payload, error) {
	upc = strings.TrimSpace(upc)
	if upc == "" {
		return providers.Payload{}, fmt.Errorf("Deezer album UPC must not be empty")
	}
	if normalized := strings.TrimLeft(upc, "0"); normalized != "" {
		upc = normalized
	}
	return c.get(ctx, "/album/upc:"+url.PathEscape(upc), nil, providers.Payload{Provider: "deezer", ProviderNamespace: "album_upc_lookup", ProviderRecordID: upc}, 12*time.Hour)
}

func (c *Client) get(ctx context.Context, path string, values url.Values, payload providers.Payload, reuse time.Duration) (providers.Payload, error) {
	requestURL, err := url.Parse(strings.TrimRight(c.config.BaseURL, "/") + path)
	if err != nil {
		return providers.Payload{}, fmt.Errorf("build Deezer URL: %w", err)
	}
	if values != nil {
		requestURL.RawQuery = values.Encode()
	}
	payload.RequestKey = strings.TrimPrefix(path, "/")
	if requestURL.RawQuery != "" {
		payload.RequestKey += "?" + requestURL.RawQuery
	}
	request, err := http.NewRequest(http.MethodGet, requestURL.String(), nil)
	if err != nil {
		return providers.Payload{}, fmt.Errorf("build Deezer request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	return c.http.DoPrepared(ctx, request, payload, func(*http.Request) error { return c.gate.Wait(ctx) }, classify(reuse))
}

func classify(reuse time.Duration) func(*providers.Payload) {
	return func(payload *providers.Payload) {
		if payload.StatusCode != http.StatusOK {
			return
		}
		var result struct {
			Error *struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
			Data json.RawMessage `json:"data"`
			ID   json.RawMessage `json:"id"`
		}
		if json.Unmarshal(payload.Body, &result) != nil {
			zero := time.Duration(0)
			payload.ReuseDurationOverride = &zero
			return
		}
		if result.Error != nil {
			duration := time.Duration(0)
			if strings.Contains(strings.ToLower(result.Error.Message), "not found") {
				duration = time.Hour
			}
			payload.ReuseDurationOverride = &duration
			return
		}
		payload.ReuseDurationOverride = &reuse
	}
}

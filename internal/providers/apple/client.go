package apple

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

var entities = map[string]string{"artist": "musicArtist", "album": "album", "track": "song"}
var musicEntities = map[string]string{"artist": "artists", "album": "albums", "track": "songs"}

type Client struct {
	config         config.AppleConfig
	http           *providers.HTTPClient
	gate           *providers.RequestGate
	developerToken string
}

func New(config config.AppleConfig) *Client {
	return newClient(config, "", providers.NewHTTPClient(30*time.Second))
}
func NewCached(config config.AppleConfig, resolver providers.PayloadResolver, developerToken string) *Client {
	return newClient(config, developerToken, providers.NewCachedHTTPClient(30*time.Second, resolver))
}
func newClient(config config.AppleConfig, developerToken string, client *providers.HTTPClient) *Client {
	gateKey := "apple:" + strings.TrimRight(config.MusicBaseURL, "/") + "|" + strings.TrimRight(config.BaseURL, "/")
	return &Client{
		config: config, developerToken: developerToken, http: client,
		gate: providers.SharedRequestGate(gateKey, config.RequestsPerSecond),
	}
}

func (c *Client) Capability() providers.Capability {
	return providers.Capability{
		Provider: "apple", EntityKind: "music_source",
		RawRetention:        providers.RetentionPolicy{Class: "provider_raw_48h", Duration: 48 * time.Hour, ObjectPrefix: "ephemeral/48h"},
		ResponseCache:       providers.ResponseCachePolicy{ReuseDuration: 12 * time.Hour, NegativeDuration: time.Hour, RedisBodyDuration: time.Hour, MaxRedisBodyBytes: 4 * 1024 * 1024},
		AcceptedIdentifiers: []providers.Identifier{{Provider: "apple", Namespace: "artist"}, {Provider: "apple", Namespace: "album"}, {Provider: "apple", Namespace: "track"}},
		Provides:            []providers.Scope{providers.ScopeIdentity, providers.ScopeTitles, providers.ScopeClassification, providers.ScopeReleases, providers.ScopeArtwork},
	}
}

func (c *Client) Collect(ctx context.Context, identifier providers.Identifier) ([]providers.Payload, error) {
	if identifier.Provider != "apple" || entities[identifier.Namespace] == "" {
		return nil, fmt.Errorf("Apple collector requires an apple artist, album, or track ID")
	}
	id, err := strconv.ParseInt(identifier.Value, 10, 64)
	if err != nil || id < 1 {
		return nil, fmt.Errorf("Apple %s collector requires a positive numeric ID", identifier.Namespace)
	}
	if c.musicToken() != "" {
		path := "/catalog/" + strings.ToLower(c.country()) + "/" + musicEntities[identifier.Namespace] + "/" + identifier.Value
		payload, collectErr := c.musicGet(ctx, path,
			url.Values{"include": {musicIncludes(identifier.Namespace)}},
			providers.Payload{Provider: "apple", ProviderNamespace: identifier.Namespace, ProviderRecordID: identifier.Value},
			12*time.Hour,
		)
		if collectErr != nil {
			return nil, collectErr
		}
		return []providers.Payload{payload}, nil
	}
	values := url.Values{"id": {identifier.Value}, "country": {c.country()}}
	if identifier.Namespace == "artist" {
		values.Set("entity", "album")
		values.Set("limit", "200")
	} else if identifier.Namespace == "album" {
		values.Set("entity", "song")
		values.Set("limit", "200")
	}
	payload, err := c.get(ctx, "/lookup", values, providers.Payload{Provider: "apple", ProviderNamespace: identifier.Namespace, ProviderRecordID: identifier.Value}, 12*time.Hour)
	if err != nil {
		return nil, err
	}
	return []providers.Payload{payload}, nil
}

func (c *Client) Search(ctx context.Context, namespace, query, country string, limit int) (providers.Payload, error) {
	entity := entities[namespace]
	query = strings.TrimSpace(query)
	if entity == "" || query == "" {
		return providers.Payload{}, fmt.Errorf("Apple search requires artist, album, or track and a query")
	}
	if limit < 1 || limit > 200 {
		limit = 25
	}
	if strings.TrimSpace(country) == "" {
		country = c.country()
	}
	if c.musicToken() != "" {
		values := url.Values{"term": {query}, "types": {musicEntities[namespace]}, "limit": {strconv.Itoa(limit)}}
		return c.musicGet(ctx, "/catalog/"+strings.ToLower(country)+"/search", values,
			providers.Payload{Provider: "apple", ProviderNamespace: namespace + "_search", ProviderRecordID: query},
			6*time.Hour,
		)
	}
	values := url.Values{"term": {query}, "media": {"music"}, "entity": {entity}, "country": {strings.ToUpper(country)}, "limit": {strconv.Itoa(limit)}}
	return c.get(ctx, "/search", values, providers.Payload{Provider: "apple", ProviderNamespace: namespace + "_search", ProviderRecordID: query}, 6*time.Hour)
}

func (c *Client) LookupAlbumByUPC(ctx context.Context, upc string) (providers.Payload, error) {
	upc = strings.TrimSpace(upc)
	if upc == "" {
		return providers.Payload{}, fmt.Errorf("Apple album UPC must not be empty")
	}
	return c.musicGet(ctx, "/catalog/"+strings.ToLower(c.country())+"/albums", url.Values{"filter[upc]": {upc}, "include": {"artists,tracks"}}, providers.Payload{Provider: "apple", ProviderNamespace: "album_upc_lookup", ProviderRecordID: upc}, 12*time.Hour)
}

func (c *Client) musicGet(ctx context.Context, path string, values url.Values, payload providers.Payload, reuse time.Duration) (providers.Payload, error) {
	requestURL, err := url.Parse(strings.TrimRight(c.config.MusicBaseURL, "/") + path)
	if err != nil {
		return providers.Payload{}, fmt.Errorf("build Apple Music URL: %w", err)
	}
	requestURL.RawQuery = values.Encode()
	payload.RequestKey = "music/" + strings.TrimPrefix(path, "/") + "?" + values.Encode()
	request, err := http.NewRequest(http.MethodGet, requestURL.String(), nil)
	if err != nil {
		return providers.Payload{}, fmt.Errorf("build Apple Music request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	return c.http.DoPrepared(ctx, request, payload, func(request *http.Request) error {
		token := c.musicToken()
		if token == "" {
			return fmt.Errorf("Apple Music requires X-Heya-Apple-API-Key or HEYA_METADATA_APPLE_DEVELOPER_TOKEN")
		}
		if err := c.gate.Wait(ctx); err != nil {
			return err
		}
		request.Header.Set("Authorization", "Bearer "+token)
		return nil
	}, classifyMusic(reuse))
}

func (c *Client) musicToken() string {
	if c.developerToken != "" {
		return c.developerToken
	}
	return c.config.DeveloperToken
}

func musicIncludes(namespace string) string {
	switch namespace {
	case "artist":
		return "albums"
	case "album":
		return "artists,tracks"
	case "track":
		return "albums,artists"
	default:
		return ""
	}
}

func classifyMusic(reuse time.Duration) func(*providers.Payload) {
	return func(payload *providers.Payload) {
		if payload.StatusCode != http.StatusOK {
			return
		}
		var result struct {
			Data   []json.RawMessage `json:"data"`
			Errors []json.RawMessage `json:"errors"`
		}
		if json.Unmarshal(payload.Body, &result) != nil || len(result.Errors) > 0 {
			zero := time.Duration(0)
			payload.ReuseDurationOverride = &zero
			return
		}
		if len(result.Data) == 0 {
			hour := time.Hour
			payload.ReuseDurationOverride = &hour
			return
		}
		payload.ReuseDurationOverride = &reuse
	}
}

func (c *Client) get(ctx context.Context, path string, values url.Values, payload providers.Payload, reuse time.Duration) (providers.Payload, error) {
	requestURL, err := url.Parse(strings.TrimRight(c.config.BaseURL, "/") + path)
	if err != nil {
		return providers.Payload{}, fmt.Errorf("build Apple URL: %w", err)
	}
	requestURL.RawQuery = values.Encode()
	payload.RequestKey = strings.TrimPrefix(path, "/") + "?" + values.Encode()
	request, err := http.NewRequest(http.MethodGet, requestURL.String(), nil)
	if err != nil {
		return providers.Payload{}, fmt.Errorf("build Apple request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	return c.http.DoPrepared(ctx, request, payload, func(*http.Request) error { return c.gate.Wait(ctx) }, classify(reuse))
}

func (c *Client) country() string {
	value := strings.ToUpper(strings.TrimSpace(c.config.Country))
	if len(value) != 2 {
		return "US"
	}
	return value
}

func classify(reuse time.Duration) func(*providers.Payload) {
	return func(payload *providers.Payload) {
		if payload.StatusCode != http.StatusOK {
			return
		}
		var result struct {
			ResultCount int               `json:"resultCount"`
			Results     []json.RawMessage `json:"results"`
		}
		if json.Unmarshal(payload.Body, &result) != nil || (result.ResultCount > 0 && len(result.Results) == 0) {
			zero := time.Duration(0)
			payload.ReuseDurationOverride = &zero
			return
		}
		if result.ResultCount == 0 {
			hour := time.Hour
			payload.ReuseDurationOverride = &hour
			return
		}
		payload.ReuseDurationOverride = &reuse
	}
}

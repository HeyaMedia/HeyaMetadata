package discogs

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

var resources = map[string]string{
	"artist":  "artists",
	"release": "releases",
	"master":  "masters",
	"label":   "labels",
}

type Client struct {
	config config.DiscogsConfig
	apiKey string
	http   *providers.HTTPClient
	gate   *providers.RequestGate
}

func New(config config.DiscogsConfig) *Client {
	return newClient(config, "", providers.NewHTTPClient(30*time.Second))
}

func NewCached(config config.DiscogsConfig, resolver providers.PayloadResolver, apiKey string) *Client {
	return newClient(config, apiKey, providers.NewCachedHTTPClient(30*time.Second, resolver))
}

func newClient(config config.DiscogsConfig, apiKey string, client *providers.HTTPClient) *Client {
	return &Client{
		config: config,
		apiKey: apiKey,
		http:   client,
		gate:   providers.SharedRequestGate("discogs:"+strings.TrimRight(config.BaseURL, "/"), config.RequestsPerSecond),
	}
}

func (c *Client) Capability() providers.Capability {
	return providers.Capability{
		Provider: "discogs", EntityKind: "music_source",
		RawRetention:  providers.RetentionPolicy{Class: "provider_raw_48h", Duration: 48 * time.Hour, ObjectPrefix: "ephemeral/48h"},
		ResponseCache: providers.ResponseCachePolicy{ReuseDuration: 12 * time.Hour, NegativeDuration: time.Hour, RedisBodyDuration: time.Hour, MaxRedisBodyBytes: 8 * 1024 * 1024},
		AcceptedIdentifiers: []providers.Identifier{
			{Provider: "discogs", Namespace: "artist"},
			{Provider: "discogs", Namespace: "release"},
			{Provider: "discogs", Namespace: "master"},
			{Provider: "discogs", Namespace: "label"},
		},
		Provides: []providers.Scope{
			providers.ScopeIdentity, providers.ScopeTitles, providers.ScopeDescriptions,
			providers.ScopeClassification, providers.ScopeReleases, providers.ScopeCredits,
			providers.ScopeArtwork,
		},
	}
}

func (c *Client) Collect(ctx context.Context, identifier providers.Identifier) ([]providers.Payload, error) {
	resource := resources[identifier.Namespace]
	if identifier.Provider != "discogs" || resource == "" {
		return nil, fmt.Errorf("Discogs collector requires an artist, release, master, or label ID")
	}
	if !positiveID(identifier.Value) {
		return nil, fmt.Errorf("Discogs %s collector requires a positive numeric ID", identifier.Namespace)
	}
	payload, err := c.get(ctx, "/"+resource+"/"+identifier.Value, nil, providers.Payload{
		Provider: "discogs", ProviderNamespace: identifier.Namespace, ProviderRecordID: identifier.Value,
	}, 12*time.Hour, false)
	if err != nil {
		return nil, err
	}
	return []providers.Payload{payload}, nil
}

func (c *Client) Search(ctx context.Context, query, kind string, perPage, page int) (providers.Payload, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return providers.Payload{}, fmt.Errorf("Discogs search query must not be empty")
	}
	if kind != "" && resources[kind] == "" {
		return providers.Payload{}, fmt.Errorf("unsupported Discogs search type %q", kind)
	}
	perPage, page = pagination(perPage, page, 25)
	values := url.Values{"q": {query}, "per_page": {strconv.Itoa(perPage)}, "page": {strconv.Itoa(page)}}
	if kind != "" {
		values.Set("type", kind)
	}
	return c.get(ctx, "/database/search", values, providers.Payload{
		Provider: "discogs", ProviderNamespace: kind + "_search", ProviderRecordID: query,
	}, 6*time.Hour, true)
}
func (c *Client) SearchReleaseByBarcode(ctx context.Context, barcode string, perPage int) (providers.Payload, error) {
	barcode = strings.TrimSpace(barcode)
	if barcode == "" {
		return providers.Payload{}, fmt.Errorf("Discogs barcode must not be empty")
	}
	if perPage < 1 || perPage > 10 {
		perPage = 5
	}
	values := url.Values{"barcode": {barcode}, "type": {"release"}, "per_page": {strconv.Itoa(perPage)}, "page": {"1"}}
	return c.get(ctx, "/database/search", values, providers.Payload{Provider: "discogs", ProviderNamespace: "release_barcode_search", ProviderRecordID: barcode}, 6*time.Hour, true)
}

func (c *Client) ArtistReleases(ctx context.Context, artistID string, perPage, page int) (providers.Payload, error) {
	return c.page(ctx, "artist_releases", "/artists/"+artistID+"/releases", artistID, perPage, page)
}

func (c *Client) MasterVersions(ctx context.Context, masterID string, perPage, page int) (providers.Payload, error) {
	return c.page(ctx, "master_versions", "/masters/"+masterID+"/versions", masterID, perPage, page)
}

func (c *Client) page(ctx context.Context, namespace, path, id string, perPage, page int) (providers.Payload, error) {
	if !positiveID(id) {
		return providers.Payload{}, fmt.Errorf("Discogs page requires a positive ID")
	}
	perPage, page = pagination(perPage, page, 100)
	values := url.Values{"per_page": {strconv.Itoa(perPage)}, "page": {strconv.Itoa(page)}}
	return c.get(ctx, path, values, providers.Payload{
		Provider: "discogs", ProviderNamespace: namespace, ProviderRecordID: id,
	}, 12*time.Hour, false)
}

func (c *Client) get(ctx context.Context, path string, values url.Values, payload providers.Payload, reuse time.Duration, requireKey bool) (providers.Payload, error) {
	requestURL, err := url.Parse(strings.TrimRight(c.config.BaseURL, "/") + path)
	if err != nil {
		return providers.Payload{}, fmt.Errorf("build Discogs URL: %w", err)
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
		return providers.Payload{}, fmt.Errorf("build Discogs request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	return c.http.DoPrepared(ctx, request, payload, func(request *http.Request) error {
		if strings.TrimSpace(c.config.UserAgent) == "" {
			return fmt.Errorf("Discogs requires HEYA_METADATA_DISCOGS_USER_AGENT")
		}
		key := c.apiKey
		if key == "" {
			key = c.config.APIKey
		}
		if requireKey && key == "" {
			return fmt.Errorf("Discogs search requires X-Heya-Discogs-API-Key or HEYA_METADATA_DISCOGS_API_KEY")
		}
		if err := c.gate.Wait(ctx); err != nil {
			return err
		}
		request.Header.Set("User-Agent", c.config.UserAgent)
		if key != "" {
			request.Header.Set("Authorization", "Discogs token="+key)
		}
		return nil
	}, classify(reuse))
}

func classify(reuse time.Duration) func(*providers.Payload) {
	return func(payload *providers.Payload) {
		if payload.StatusCode != http.StatusOK {
			return
		}
		var result map[string]json.RawMessage
		if json.Unmarshal(payload.Body, &result) != nil || result["message"] != nil {
			zero := time.Duration(0)
			payload.ReuseDurationOverride = &zero
			return
		}
		payload.ReuseDurationOverride = &reuse
	}
}

func positiveID(value string) bool {
	id, err := strconv.ParseInt(value, 10, 64)
	return err == nil && id > 0
}

func pagination(perPage, page, defaultPerPage int) (int, int) {
	if perPage < 1 || perPage > 100 {
		perPage = defaultPerPage
	}
	if page < 1 {
		page = 1
	}
	return perPage, page
}

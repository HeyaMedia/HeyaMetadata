package wikidata

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

var entityPattern = regexp.MustCompile(`^[QP][1-9][0-9]*$`)

type Client struct {
	config config.WikidataConfig
	http   *providers.HTTPClient
	gate   *providers.RequestGate
}

func New(config config.WikidataConfig) *Client {
	return newClient(config, providers.NewHTTPClient(30*time.Second))
}

func NewCached(config config.WikidataConfig, resolver providers.PayloadResolver) *Client {
	return newClient(config, providers.NewCachedHTTPClient(30*time.Second, resolver))
}

func newClient(config config.WikidataConfig, client *providers.HTTPClient) *Client {
	return &Client{
		config: config,
		http:   client,
		gate:   providers.SharedRequestGate("wikidata:"+strings.TrimRight(config.BaseURL, "/"), config.RequestsPerSecond),
	}
}

func (c *Client) Capability() providers.Capability {
	return providers.Capability{
		Provider: "wikidata", EntityKind: "knowledge_source",
		RawRetention:        providers.RetentionPolicy{Class: "provider_raw_48h", Duration: 48 * time.Hour, ObjectPrefix: "ephemeral/48h"},
		ResponseCache:       providers.ResponseCachePolicy{ReuseDuration: 24 * time.Hour, NegativeDuration: time.Hour, RedisBodyDuration: time.Hour, MaxRedisBodyBytes: 8 * 1024 * 1024},
		AcceptedIdentifiers: []providers.Identifier{{Provider: "wikidata", Namespace: "entity"}},
		Provides: []providers.Scope{
			providers.ScopeIdentity, providers.ScopeTitles, providers.ScopeDescriptions,
			providers.ScopeClassification, providers.ScopeReleases, providers.ScopeArtwork,
		},
	}
}

func (c *Client) Collect(ctx context.Context, identifier providers.Identifier) ([]providers.Payload, error) {
	id := strings.ToUpper(strings.TrimSpace(identifier.Value))
	if identifier.Provider != "wikidata" || identifier.Namespace != "entity" || !entityPattern.MatchString(id) {
		return nil, fmt.Errorf("Wikidata collector requires a valid Q- or P-entity ID")
	}
	requestURL, err := url.Parse(strings.TrimRight(c.config.BaseURL, "/") + "/wiki/Special:EntityData/" + id + ".json")
	if err != nil {
		return nil, fmt.Errorf("build Wikidata entity URL: %w", err)
	}
	payload, err := c.get(ctx, requestURL, providers.Payload{
		Provider: "wikidata", ProviderNamespace: "entity", ProviderRecordID: id,
		RequestKey: "entity/" + id + ".json",
	}, classifyEntity(id))
	if err != nil {
		return nil, err
	}
	return []providers.Payload{payload}, nil
}

func (c *Client) Search(ctx context.Context, query, language string, limit, continueAt int) (providers.Payload, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return providers.Payload{}, fmt.Errorf("Wikidata search query must not be empty")
	}
	if language == "" {
		language = "en"
	}
	if limit < 1 || limit > 50 {
		limit = 20
	}
	if continueAt < 0 {
		continueAt = 0
	}
	values := url.Values{
		"action": {"wbsearchentities"}, "format": {"json"}, "type": {"item"},
		"search": {query}, "language": {language}, "uselang": {language},
		"limit": {strconv.Itoa(limit)}, "continue": {strconv.Itoa(continueAt)},
	}
	requestURL, err := url.Parse(strings.TrimRight(c.config.BaseURL, "/") + "/w/api.php")
	if err != nil {
		return providers.Payload{}, fmt.Errorf("build Wikidata search URL: %w", err)
	}
	requestURL.RawQuery = values.Encode()
	return c.get(ctx, requestURL, providers.Payload{
		Provider: "wikidata", ProviderNamespace: "entity_search", ProviderRecordID: query,
		RequestKey: "w/api.php?" + values.Encode(),
	}, classifySearch)
}

func (c *Client) get(ctx context.Context, requestURL *url.URL, payload providers.Payload, classify func(*providers.Payload)) (providers.Payload, error) {
	request, err := http.NewRequest(http.MethodGet, requestURL.String(), nil)
	if err != nil {
		return providers.Payload{}, fmt.Errorf("build Wikidata request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	return c.http.DoPrepared(ctx, request, payload, func(request *http.Request) error {
		if strings.TrimSpace(c.config.UserAgent) == "" {
			return fmt.Errorf("Wikidata requires HEYA_METADATA_WIKIDATA_USER_AGENT")
		}
		if err := c.gate.Wait(ctx); err != nil {
			return err
		}
		request.Header.Set("User-Agent", c.config.UserAgent)
		return nil
	}, classify)
}

func classifyEntity(expectedID string) func(*providers.Payload) {
	return func(payload *providers.Payload) {
		if payload.StatusCode != http.StatusOK {
			return
		}
		var result struct {
			Entities map[string]struct {
				ID      string          `json:"id"`
				Missing json.RawMessage `json:"missing"`
			} `json:"entities"`
		}
		if json.Unmarshal(payload.Body, &result) != nil {
			zero := time.Duration(0)
			payload.ReuseDurationOverride = &zero
			return
		}
		entity, ok := result.Entities[expectedID]
		if !ok || entity.ID != expectedID || entity.Missing != nil {
			hour := time.Hour
			payload.ReuseDurationOverride = &hour
		}
	}
}

func classifySearch(payload *providers.Payload) {
	if payload.StatusCode != http.StatusOK {
		return
	}
	var result struct {
		Success int               `json:"success"`
		Search  []json.RawMessage `json:"search"`
		Error   json.RawMessage   `json:"error"`
	}
	if json.Unmarshal(payload.Body, &result) != nil || result.Error != nil {
		zero := time.Duration(0)
		payload.ReuseDurationOverride = &zero
		return
	}
	duration := 6 * time.Hour
	if len(result.Search) == 0 {
		duration = time.Hour
	}
	payload.ReuseDurationOverride = &duration
}

// Package thexem provides cached access to TheXEM's cross-numbering maps.
package thexem

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
	config config.TheXEMConfig
	http   *providers.HTTPClient
	gate   *providers.RequestGate
}

func New(config config.TheXEMConfig) *Client {
	return newClient(config, providers.NewHTTPClient(30*time.Second))
}

func NewCached(config config.TheXEMConfig, resolver providers.PayloadResolver) *Client {
	return newClient(config, providers.NewCachedHTTPClient(30*time.Second, resolver))
}

func newClient(config config.TheXEMConfig, client *providers.HTTPClient) *Client {
	return &Client{
		config: config,
		http:   client,
		gate:   providers.SharedRequestGate("thexem:"+strings.TrimRight(config.BaseURL, "/"), config.RequestsPerSecond),
	}
}

func (c *Client) Capability() providers.Capability {
	return providers.Capability{
		Provider:   "thexem",
		EntityKind: "episodic_mapping",
		RawRetention: providers.RetentionPolicy{
			Class: "provider_raw_48h", Duration: 48 * time.Hour, ObjectPrefix: "ephemeral/48h",
		},
		ResponseCache: providers.ResponseCachePolicy{
			ReuseDuration: 24 * time.Hour, NegativeDuration: 6 * time.Hour,
			RedisBodyDuration: time.Hour, MaxRedisBodyBytes: 4 * 1024 * 1024,
		},
		AcceptedIdentifiers: []providers.Identifier{{Provider: "tvdb", Namespace: "series"}},
		Provides:            []providers.Scope{providers.ScopeEpisodeNumbering, providers.ScopeTitles},
	}
}

// Collect returns the episode cross-numbering table and TheXEM's curated
// aliases for one TVDB series. TVDB is the stable bridge exposed by TMDB and
// works even when the caller cannot or should not query the TVDB API itself.
func (c *Client) Collect(ctx context.Context, identifier providers.Identifier) ([]providers.Payload, error) {
	if identifier.Provider != "tvdb" || identifier.Namespace != "series" || !positiveID(identifier.Value) {
		return nil, fmt.Errorf("TheXEM collector requires a positive tvdb.series ID")
	}
	mapping, err := c.get(ctx, "/map/all", url.Values{
		"id": {identifier.Value}, "origin": {"tvdb"},
	}, "mapping", identifier.Value)
	if err != nil {
		return nil, err
	}
	result := []providers.Payload{mapping}
	if mapping.StatusCode != http.StatusOK {
		return result, nil
	}
	names, err := c.get(ctx, "/map/names", url.Values{
		"id": {identifier.Value}, "origin": {"tvdb"}, "defaultNames": {"1"},
	}, "names", identifier.Value)
	if err != nil {
		return result, err
	}
	return append(result, names), nil
}

func (c *Client) get(ctx context.Context, path string, values url.Values, namespace, id string) (providers.Payload, error) {
	requestURL, err := url.Parse(strings.TrimRight(c.config.BaseURL, "/") + path)
	if err != nil {
		return providers.Payload{}, fmt.Errorf("build TheXEM URL: %w", err)
	}
	requestURL.RawQuery = values.Encode()
	request, err := http.NewRequest(http.MethodGet, requestURL.String(), nil)
	if err != nil {
		return providers.Payload{}, fmt.Errorf("build TheXEM request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", c.config.UserAgent)
	payload := providers.Payload{
		Provider: "thexem", ProviderNamespace: namespace, ProviderRecordID: id,
		RequestKey: strings.TrimPrefix(path, "/") + "?" + requestURL.RawQuery,
	}
	return c.http.DoPrepared(ctx, request, payload, func(*http.Request) error {
		return c.gate.Wait(ctx)
	}, classifyResponse)
}

func classifyResponse(payload *providers.Payload) {
	if payload.StatusCode != http.StatusOK {
		return
	}
	var envelope struct {
		Result string          `json:"result"`
		Data   json.RawMessage `json:"data"`
	}
	if json.Unmarshal(payload.Body, &envelope) != nil || !strings.EqualFold(envelope.Result, "success") || len(envelope.Data) == 0 || string(envelope.Data) == "null" {
		zero := time.Duration(0)
		payload.ReuseDurationOverride = &zero
		return
	}
	if string(envelope.Data) == "[]" || string(envelope.Data) == "{}" {
		negative := 6 * time.Hour
		payload.ReuseDurationOverride = &negative
	}
}

func positiveID(value string) bool {
	id, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	return err == nil && id > 0
}

package openopus

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
	config config.OpenOpusConfig
	http   *providers.HTTPClient
	gate   *providers.RequestGate
}

func New(config config.OpenOpusConfig) *Client {
	return newClient(config, providers.NewHTTPClient(30*time.Second))
}

func NewCached(config config.OpenOpusConfig, resolver providers.PayloadResolver) *Client {
	return newClient(config, providers.NewCachedHTTPClient(30*time.Second, resolver))
}

func newClient(config config.OpenOpusConfig, client *providers.HTTPClient) *Client {
	return &Client{
		config: config,
		http:   client,
		gate:   providers.SharedRequestGate("openopus:"+strings.TrimRight(config.BaseURL, "/"), config.RequestsPerSecond),
	}
}

func (c *Client) Capability() providers.Capability {
	return providers.Capability{
		Provider: "openopus", EntityKind: "classical_music_source",
		RawRetention:  providers.RetentionPolicy{Class: "provider_raw_48h", Duration: 48 * time.Hour, ObjectPrefix: "ephemeral/48h"},
		ResponseCache: providers.ResponseCachePolicy{ReuseDuration: 24 * time.Hour, NegativeDuration: time.Hour, RedisBodyDuration: time.Hour, MaxRedisBodyBytes: 16 * 1024 * 1024},
		AcceptedIdentifiers: []providers.Identifier{
			{Provider: "openopus", Namespace: "composer"},
			{Provider: "openopus", Namespace: "work"},
		},
		Provides: []providers.Scope{
			providers.ScopeIdentity, providers.ScopeTitles, providers.ScopeClassification,
			providers.ScopeReleases, providers.ScopeArtwork,
		},
	}
}

func (c *Client) Collect(ctx context.Context, identifier providers.Identifier) ([]providers.Payload, error) {
	if identifier.Provider != "openopus" || (identifier.Namespace != "composer" && identifier.Namespace != "work") || !positiveID(identifier.Value) {
		return nil, fmt.Errorf("Open Opus collector requires a positive composer or work ID")
	}
	if identifier.Namespace == "work" {
		payload, err := c.get(ctx, "/work/detail/"+identifier.Value+".json", providers.Payload{
			Provider: "openopus", ProviderNamespace: "work", ProviderRecordID: identifier.Value,
		}, 24*time.Hour)
		if err != nil {
			return nil, err
		}
		return []providers.Payload{payload}, nil
	}
	composer, err := c.get(ctx, "/composer/list/ids/"+identifier.Value+".json", providers.Payload{
		Provider: "openopus", ProviderNamespace: "composer", ProviderRecordID: identifier.Value,
	}, 24*time.Hour)
	if err != nil {
		return nil, err
	}
	payloads := []providers.Payload{composer}
	if composer.StatusCode != http.StatusOK || responseFailed(composer.Body) {
		return payloads, nil
	}
	works, err := c.get(ctx, "/work/list/composer/"+identifier.Value+"/genre/all.json", providers.Payload{
		Provider: "openopus", ProviderNamespace: "composer_works", ProviderRecordID: identifier.Value,
	}, 24*time.Hour)
	if err != nil {
		return payloads, err
	}
	return append(payloads, works), nil
}

func (c *Client) SearchComposer(ctx context.Context, query string) (providers.Payload, error) {
	query = strings.TrimSpace(query)
	if len([]rune(query)) < 4 {
		return providers.Payload{}, fmt.Errorf("Open Opus composer search requires at least four characters")
	}
	return c.get(ctx, "/composer/list/search/"+url.PathEscape(query)+".json", providers.Payload{
		Provider: "openopus", ProviderNamespace: "composer_search", ProviderRecordID: query,
	}, 6*time.Hour)
}

func (c *Client) OmniSearch(ctx context.Context, query string, offset int) (providers.Payload, error) {
	query = strings.TrimSpace(query)
	if len([]rune(query)) < 4 {
		return providers.Payload{}, fmt.Errorf("Open Opus omnisearch requires at least four characters")
	}
	if offset < 0 {
		offset = 0
	}
	return c.get(ctx, "/omnisearch/"+url.PathEscape(query)+"/"+strconv.Itoa(offset)+".json", providers.Payload{
		Provider: "openopus", ProviderNamespace: "omnisearch", ProviderRecordID: query,
	}, 6*time.Hour)
}

func (c *Client) get(ctx context.Context, path string, payload providers.Payload, reuse time.Duration) (providers.Payload, error) {
	requestURL, err := url.Parse(strings.TrimRight(c.config.BaseURL, "/") + path)
	if err != nil {
		return providers.Payload{}, fmt.Errorf("build Open Opus URL: %w", err)
	}
	payload.RequestKey = strings.TrimPrefix(path, "/")
	request, err := http.NewRequest(http.MethodGet, requestURL.String(), nil)
	if err != nil {
		return providers.Payload{}, fmt.Errorf("build Open Opus request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	return c.http.DoPrepared(ctx, request, payload, func(*http.Request) error {
		return c.gate.Wait(ctx)
	}, classify(reuse))
}

func classify(reuse time.Duration) func(*providers.Payload) {
	return func(payload *providers.Payload) {
		if payload.StatusCode != http.StatusOK {
			return
		}
		var result struct {
			Status struct {
				Success string `json:"success"`
				Rows    int    `json:"rows"`
			} `json:"status"`
		}
		if json.Unmarshal(payload.Body, &result) != nil || result.Status.Success == "" {
			zero := time.Duration(0)
			payload.ReuseDurationOverride = &zero
			return
		}
		if !strings.EqualFold(result.Status.Success, "true") {
			duration := time.Duration(0)
			if result.Status.Rows == 0 {
				duration = time.Hour
			}
			payload.ReuseDurationOverride = &duration
			return
		}
		payload.ReuseDurationOverride = &reuse
	}
}

func responseFailed(body []byte) bool {
	var result struct {
		Status struct {
			Success string `json:"success"`
		} `json:"status"`
	}
	return json.Unmarshal(body, &result) != nil || !strings.EqualFold(result.Status.Success, "true")
}

func positiveID(value string) bool {
	id, err := strconv.ParseInt(value, 10, 64)
	return err == nil && id > 0
}

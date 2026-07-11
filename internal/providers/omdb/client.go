package omdb

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/config"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers"
)

var imdbTitlePattern = regexp.MustCompile(`^tt[0-9]{7,10}$`)

type Client struct {
	config config.OMDBConfig
	apiKey string
	http   *providers.HTTPClient
}

func New(config config.OMDBConfig) *Client {
	return &Client{config: config, http: providers.NewHTTPClient(30 * time.Second)}
}

func NewCached(config config.OMDBConfig, resolver providers.PayloadResolver, apiKey string) *Client {
	return &Client{config: config, apiKey: apiKey, http: providers.NewCachedHTTPClient(30*time.Second, resolver)}
}

func (c *Client) Capability() providers.Capability {
	return providers.Capability{
		Provider: "omdb", EntityKind: "movie",
		RawRetention: providers.RetentionPolicy{
			Class: "provider_raw_48h", Duration: 48 * time.Hour, ObjectPrefix: "ephemeral/48h",
		},
		ResponseCache: providers.ResponseCachePolicy{
			ReuseDuration: 24 * time.Hour, NegativeDuration: time.Hour,
			RedisBodyDuration: time.Hour, MaxRedisBodyBytes: 256 * 1024,
		},
		AcceptedIdentifiers: []providers.Identifier{{Provider: "imdb", Namespace: "title"}},
		Provides: []providers.Scope{
			providers.ScopeIdentity, providers.ScopeTitles, providers.ScopeDescriptions,
			providers.ScopeClassification, providers.ScopeReleases, providers.ScopeRatings,
		},
	}
}

func (c *Client) Collect(ctx context.Context, identifier providers.Identifier) ([]providers.Payload, error) {
	if identifier.Provider != "imdb" || identifier.Namespace != "title" || !imdbTitlePattern.MatchString(identifier.Value) {
		return nil, fmt.Errorf("OMDb movie collector requires a valid IMDb title ID")
	}
	requestURL, err := url.Parse(c.config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("build OMDb URL: %w", err)
	}
	apiKey := c.apiKey
	if apiKey == "" {
		apiKey = c.config.APIKey
	}
	query := requestURL.Query()
	query.Set("i", identifier.Value)
	query.Set("plot", "full")
	query.Set("r", "json")
	query.Set("type", "movie")
	if apiKey != "" {
		query.Set("apikey", apiKey)
	}
	requestURL.RawQuery = query.Encode()
	request, err := http.NewRequest(http.MethodGet, requestURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("build OMDb request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	payload, err := c.http.DoClassified(ctx, request, providers.Payload{
		Provider: "omdb", ProviderNamespace: "imdb_title", ProviderRecordID: identifier.Value,
		RequestKey: "detail/" + identifier.Value + "?plot=full&type=movie",
	}, func() error {
		if apiKey == "" {
			return fmt.Errorf("OMDb requires X-Heya-OMDB-API-Key or HEYA_METADATA_OMDB_API_KEY")
		}
		return nil
	}, classifyReuse)
	if err != nil {
		return nil, err
	}
	return []providers.Payload{payload}, nil
}

func classifyReuse(payload *providers.Payload) {
	if payload.StatusCode < 200 || payload.StatusCode >= 300 {
		return
	}
	var envelope response
	if json.Unmarshal(payload.Body, &envelope) != nil {
		duration := time.Duration(0)
		payload.ReuseDurationOverride = &duration
		return
	}
	if strings.EqualFold(envelope.Response, "False") {
		duration := time.Duration(0)
		if strings.Contains(strings.ToLower(envelope.Error), "not found") {
			duration = time.Hour
		}
		payload.ReuseDurationOverride = &duration
	}
}

func ResponseError(body []byte) (string, bool) {
	var envelope response
	if json.Unmarshal(body, &envelope) != nil || !strings.EqualFold(envelope.Response, "False") {
		return "", false
	}
	return strings.TrimSpace(envelope.Error), true
}

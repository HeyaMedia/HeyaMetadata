package fanart

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
	config    config.FanartConfig
	clientKey string
	http      *providers.HTTPClient
}

func New(config config.FanartConfig) *Client {
	return &Client{config: config, http: providers.NewHTTPClient(30 * time.Second)}
}

func NewCached(config config.FanartConfig, resolver providers.PayloadResolver, clientKey string) *Client {
	return &Client{config: config, clientKey: clientKey, http: providers.NewCachedHTTPClient(30*time.Second, resolver)}
}

func (c *Client) Capability() providers.Capability {
	return providers.Capability{
		Provider: "fanart", EntityKind: "movie",
		RawRetention:        providers.RetentionPolicy{Class: "provider_raw_48h", Duration: 48 * time.Hour, ObjectPrefix: "ephemeral/48h"},
		ResponseCache:       providers.ResponseCachePolicy{ReuseDuration: 24 * time.Hour, NegativeDuration: time.Hour, RedisBodyDuration: time.Hour, MaxRedisBodyBytes: 4 * 1024 * 1024},
		AcceptedIdentifiers: []providers.Identifier{{Provider: "tmdb", Namespace: "movie"}},
		Provides:            []providers.Scope{providers.ScopeIdentity, providers.ScopeArtwork},
	}
}

func (c *Client) Collect(ctx context.Context, identifier providers.Identifier) ([]providers.Payload, error) {
	if identifier.Provider != "tmdb" || identifier.Namespace != "movie" {
		return nil, fmt.Errorf("Fanart.tv movie collector requires a TMDB movie ID")
	}
	tmdbID, err := strconv.ParseInt(identifier.Value, 10, 64)
	if err != nil || tmdbID < 1 {
		return nil, fmt.Errorf("Fanart.tv movie collector requires a positive TMDB movie ID")
	}
	requestURL, err := url.Parse(strings.TrimRight(c.config.BaseURL, "/") + "/movies/" + strconv.FormatInt(tmdbID, 10))
	if err != nil {
		return nil, fmt.Errorf("build Fanart.tv URL: %w", err)
	}
	query := requestURL.Query()
	if c.config.APIKey != "" {
		query.Set("api_key", c.config.APIKey)
	}
	if c.clientKey != "" {
		query.Set("client_key", c.clientKey)
	}
	requestURL.RawQuery = query.Encode()
	request, err := http.NewRequest(http.MethodGet, requestURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("build Fanart.tv request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	payload, err := c.http.DoClassified(ctx, request, providers.Payload{
		Provider: "fanart", ProviderNamespace: "movie", ProviderRecordID: identifier.Value,
		RequestKey: "movies/" + identifier.Value,
	}, func() error {
		if c.config.APIKey == "" && c.clientKey == "" {
			return fmt.Errorf("Fanart.tv requires X-Heya-Fanart-API-Key or HEYA_METADATA_FANART_API_KEY")
		}
		return nil
	}, classifyReuse)
	if err != nil {
		return nil, err
	}
	return []providers.Payload{payload}, nil
}

func classifyReuse(payload *providers.Payload) {
	if payload.StatusCode != http.StatusOK {
		return
	}
	var response movieResponse
	if err := json.Unmarshal(payload.Body, &response); err != nil {
		zero := time.Duration(0)
		payload.ReuseDurationOverride = &zero
		return
	}
	if response.TMDBID == "" && response.IMDBID == "" && imageCount(response) == 0 {
		hour := time.Hour
		payload.ReuseDurationOverride = &hour
	}
}

func imageCount(response movieResponse) int {
	return len(response.MoviePosters) + len(response.MovieBackgrounds) + len(response.HDMovieLogos) +
		len(response.MovieLogos) + len(response.MovieBanners) + len(response.HDMovieClearArts) +
		len(response.MovieArts) + len(response.MovieThumbs) + len(response.MovieDiscs)
}

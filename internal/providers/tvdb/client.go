package tvdb

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/config"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers"
	"github.com/redis/go-redis/v9"
)

type Client struct {
	config config.TVDBConfig
	apiKey string
	redis  *redis.Client
	http   *providers.HTTPClient
	auth   *http.Client
	mutex  sync.Mutex
	token  string
	expiry time.Time
}

func New(config config.TVDBConfig) *Client {
	return &Client{config: config, http: providers.NewHTTPClient(30 * time.Second), auth: &http.Client{Timeout: 30 * time.Second}}
}

func NewCached(config config.TVDBConfig, resolver providers.PayloadResolver, apiKey string, redisClient *redis.Client) *Client {
	return &Client{config: config, apiKey: apiKey, redis: redisClient, http: providers.NewCachedHTTPClient(30*time.Second, resolver), auth: &http.Client{Timeout: 30 * time.Second}}
}

func (c *Client) Capability() providers.Capability {
	return providers.Capability{
		Provider: "tvdb", EntityKind: "movie",
		RawRetention:        providers.RetentionPolicy{Class: "provider_raw_48h", Duration: 48 * time.Hour, ObjectPrefix: "ephemeral/48h"},
		ResponseCache:       providers.ResponseCachePolicy{ReuseDuration: 24 * time.Hour, NegativeDuration: time.Hour, RedisBodyDuration: time.Hour, MaxRedisBodyBytes: 2 * 1024 * 1024},
		AcceptedIdentifiers: []providers.Identifier{{Provider: "imdb", Namespace: "title"}},
		Provides: []providers.Scope{
			providers.ScopeIdentity, providers.ScopeTitles, providers.ScopeDescriptions,
			providers.ScopeClassification, providers.ScopeReleases, providers.ScopeCredits,
			providers.ScopeArtwork,
		},
	}
}

func (c *Client) Collect(ctx context.Context, identifier providers.Identifier) ([]providers.Payload, error) {
	if identifier.Provider != "imdb" || identifier.Namespace != "title" || !strings.HasPrefix(identifier.Value, "tt") {
		return nil, fmt.Errorf("TVDB movie collector requires an IMDb title ID")
	}
	search, err := c.get(ctx, "/search/remoteid/"+url.PathEscape(identifier.Value), providers.Payload{
		Provider: "tvdb", ProviderNamespace: "remote_id_search", ProviderRecordID: identifier.Value,
		RequestKey: "search/remoteid/" + identifier.Value,
	}, classifyRemoteSearch)
	if err != nil {
		return nil, err
	}
	payloads := []providers.Payload{search}
	if search.StatusCode != http.StatusOK {
		return payloads, nil
	}
	var result envelope[[]remoteSearchResult]
	if err := json.Unmarshal(search.Body, &result); err != nil {
		return payloads, fmt.Errorf("decode TVDB remote-ID search: %w", err)
	}
	var tvdbID int64
	for _, candidate := range result.Data {
		if candidate.Movie != nil && candidate.Movie.ID > 0 {
			tvdbID = candidate.Movie.ID
			break
		}
	}
	if tvdbID == 0 {
		return payloads, nil
	}
	detail, err := c.get(ctx, "/movies/"+strconv.FormatInt(tvdbID, 10)+"/extended", providers.Payload{
		Provider: "tvdb", ProviderNamespace: "movie", ProviderRecordID: strconv.FormatInt(tvdbID, 10),
		RequestKey: "movies/" + strconv.FormatInt(tvdbID, 10) + "/extended",
	}, nil)
	if err != nil {
		return payloads, err
	}
	return append(payloads, detail), nil
}

// CollectSeries fetches the extended series document including TVDB's episode
// list. Its identifier must already be supported by explicit cross-ID evidence.
func (c *Client) CollectSeries(ctx context.Context, identifier providers.Identifier) ([]providers.Payload, error) {
	if identifier.Provider != "tvdb" || identifier.Namespace != "series" {
		return nil, fmt.Errorf("TVDB series collector cannot accept %s.%s", identifier.Provider, identifier.Namespace)
	}
	id, err := strconv.ParseInt(identifier.Value, 10, 64)
	if err != nil || id < 1 {
		return nil, fmt.Errorf("invalid TVDB series ID %q", identifier.Value)
	}
	payload, err := c.get(ctx, "/series/"+identifier.Value+"/extended?meta=episodes", providers.Payload{
		Provider: "tvdb", ProviderNamespace: "series", ProviderRecordID: identifier.Value,
		RequestKey: "series/" + identifier.Value + "/extended?meta=episodes",
	}, nil)
	if err != nil {
		return nil, err
	}
	return []providers.Payload{payload}, nil
}

func (c *Client) get(ctx context.Context, endpoint string, payload providers.Payload, classify func(*providers.Payload)) (providers.Payload, error) {
	requestURL := strings.TrimRight(c.config.BaseURL, "/") + endpoint
	request, err := http.NewRequest(http.MethodGet, requestURL, nil)
	if err != nil {
		return providers.Payload{}, fmt.Errorf("build TVDB request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	return c.http.DoPrepared(ctx, request, payload, func(request *http.Request) error {
		token, err := c.getToken(ctx)
		if err != nil {
			return err
		}
		request.Header.Set("Authorization", "Bearer "+token)
		return nil
	}, func(payload *providers.Payload) {
		if payload.StatusCode == http.StatusUnauthorized {
			c.invalidateToken(ctx)
		}
		if classify != nil {
			classify(payload)
		}
	})
}

func (c *Client) getToken(ctx context.Context) (string, error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if c.token != "" && time.Now().Before(c.expiry) {
		return c.token, nil
	}
	apiKey := c.apiKey
	if apiKey == "" {
		apiKey = c.config.APIKey
	}
	if apiKey == "" {
		return "", fmt.Errorf("TVDB requires X-Heya-TVDB-API-Key or HEYA_METADATA_TVDB_API_KEY")
	}
	cacheKey := c.tokenCacheKey(apiKey)
	if c.apiKey == "" && c.redis != nil {
		if token, err := c.redis.Get(ctx, cacheKey).Result(); err == nil && token != "" {
			c.token, c.expiry = token, time.Now().Add(24*time.Hour)
			return token, nil
		}
	}
	body, _ := json.Marshal(map[string]string{"apikey": apiKey})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.config.BaseURL, "/")+"/login", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("build TVDB login request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := c.auth.Do(request)
	if err != nil {
		return "", fmt.Errorf("send TVDB login request: %w", err)
	}
	defer response.Body.Close()
	responseBody, _ := io.ReadAll(io.LimitReader(response.Body, 1024*1024))
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("TVDB login returned HTTP %d", response.StatusCode)
	}
	var result envelope[struct {
		Token string `json:"token"`
	}]
	if err := json.Unmarshal(responseBody, &result); err != nil || result.Data.Token == "" {
		return "", fmt.Errorf("decode TVDB login response")
	}
	c.token, c.expiry = result.Data.Token, time.Now().Add(25*24*time.Hour)
	if c.apiKey == "" && c.redis != nil {
		_ = c.redis.Set(ctx, cacheKey, c.token, 25*24*time.Hour).Err()
	}
	return c.token, nil
}

func (c *Client) invalidateToken(ctx context.Context) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.token, c.expiry = "", time.Time{}
	if c.apiKey == "" && c.redis != nil && c.config.APIKey != "" {
		_ = c.redis.Del(context.WithoutCancel(ctx), c.tokenCacheKey(c.config.APIKey)).Err()
	}
}

func (c *Client) tokenCacheKey(apiKey string) string {
	digest := sha256.Sum256([]byte(apiKey))
	return "heya:metadata:v1:provider-token:tvdb:" + hex.EncodeToString(digest[:])
}

func classifyRemoteSearch(payload *providers.Payload) {
	if payload.StatusCode != http.StatusOK {
		return
	}
	var result envelope[[]remoteSearchResult]
	if json.Unmarshal(payload.Body, &result) != nil {
		zero := time.Duration(0)
		payload.ReuseDurationOverride = &zero
		return
	}
	if len(result.Data) == 0 {
		hour := time.Hour
		payload.ReuseDurationOverride = &hour
	}
}

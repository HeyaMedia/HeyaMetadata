package lastfm

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

var mbidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
var methods = map[string]string{
	"artist":        "artist.getInfo",
	"release_group": "album.getInfo",
	"recording":     "track.getInfo",
}

type Client struct {
	config config.LastFMConfig
	apiKey string
	http   *providers.HTTPClient
	gate   *providers.RequestGate
}

func New(config config.LastFMConfig) *Client {
	return newClient(config, "", providers.NewHTTPClient(30*time.Second))
}

func NewCached(config config.LastFMConfig, resolver providers.PayloadResolver, apiKey string) *Client {
	return newClient(config, apiKey, providers.NewCachedHTTPClient(30*time.Second, resolver))
}

func newClient(config config.LastFMConfig, apiKey string, client *providers.HTTPClient) *Client {
	return &Client{
		config: config,
		apiKey: apiKey,
		http:   client,
		gate:   providers.SharedRequestGate("lastfm:"+strings.TrimRight(config.BaseURL, "/"), config.RequestsPerSecond),
	}
}

func (c *Client) Capability() providers.Capability {
	return providers.Capability{
		Provider: "lastfm", EntityKind: "music_source",
		RawRetention:  providers.RetentionPolicy{Class: "provider_raw_48h", Duration: 48 * time.Hour, ObjectPrefix: "ephemeral/48h"},
		ResponseCache: providers.ResponseCachePolicy{ReuseDuration: 12 * time.Hour, NegativeDuration: time.Hour, RedisBodyDuration: time.Hour, MaxRedisBodyBytes: 4 * 1024 * 1024},
		AcceptedIdentifiers: []providers.Identifier{
			{Provider: "musicbrainz", Namespace: "artist"},
			{Provider: "musicbrainz", Namespace: "release_group"},
			{Provider: "musicbrainz", Namespace: "recording"},
		},
		Provides: []providers.Scope{
			providers.ScopeIdentity, providers.ScopeTitles, providers.ScopeDescriptions,
			providers.ScopeClassification, providers.ScopeRatings, providers.ScopeArtwork,
			providers.ScopeRecommendations,
		},
	}
}

func (c *Client) Collect(ctx context.Context, identifier providers.Identifier) ([]providers.Payload, error) {
	method := methods[identifier.Namespace]
	if identifier.Provider != "musicbrainz" || method == "" || !mbidPattern.MatchString(identifier.Value) {
		return nil, fmt.Errorf("Last.fm collector requires a MusicBrainz artist, release-group, or recording MBID")
	}
	mbid := strings.ToLower(identifier.Value)
	payload, err := c.call(ctx, method, url.Values{"mbid": {mbid}, "autocorrect": {"1"}}, providers.Payload{
		Provider: "lastfm", ProviderNamespace: identifier.Namespace, ProviderRecordID: mbid,
	}, 12*time.Hour)
	if err != nil {
		return nil, err
	}
	return []providers.Payload{payload}, nil
}

func (c *Client) Search(ctx context.Context, namespace, query string, limit, page int) (providers.Payload, error) {
	if namespace != "artist" && namespace != "album" && namespace != "track" {
		return providers.Payload{}, fmt.Errorf("unsupported Last.fm search namespace %q", namespace)
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return providers.Payload{}, fmt.Errorf("Last.fm search query must not be empty")
	}
	limit, page = pagination(limit, page, 30)
	values := url.Values{namespace: {query}, "limit": {strconv.Itoa(limit)}, "page": {strconv.Itoa(page)}}
	return c.call(ctx, namespace+".search", values, providers.Payload{
		Provider: "lastfm", ProviderNamespace: namespace + "_search", ProviderRecordID: query,
	}, 6*time.Hour)
}

func (c *Client) ArtistTopAlbums(ctx context.Context, mbid string, limit, page int) (providers.Payload, error) {
	return c.artistPage(ctx, "artist.getTopAlbums", "artist_top_albums", mbid, limit, page, 12*time.Hour)
}

func (c *Client) ArtistSimilar(ctx context.Context, mbid string, limit int) (providers.Payload, error) {
	if !mbidPattern.MatchString(mbid) {
		return providers.Payload{}, fmt.Errorf("Last.fm similar artists requires an artist MBID")
	}
	if limit < 1 || limit > 1000 {
		limit = 20
	}
	mbid = strings.ToLower(mbid)
	return c.call(ctx, "artist.getSimilar", url.Values{
		"mbid": {mbid}, "autocorrect": {"1"}, "limit": {strconv.Itoa(limit)},
	}, providers.Payload{
		Provider: "lastfm", ProviderNamespace: "artist_similar", ProviderRecordID: mbid,
	}, 10*time.Minute)
}

func (c *Client) artistPage(ctx context.Context, method, namespace, mbid string, limit, page int, reuse time.Duration) (providers.Payload, error) {
	if !mbidPattern.MatchString(mbid) {
		return providers.Payload{}, fmt.Errorf("Last.fm artist method requires an artist MBID")
	}
	limit, page = pagination(limit, page, 50)
	mbid = strings.ToLower(mbid)
	return c.call(ctx, method, url.Values{
		"mbid": {mbid}, "autocorrect": {"1"}, "limit": {strconv.Itoa(limit)}, "page": {strconv.Itoa(page)},
	}, providers.Payload{
		Provider: "lastfm", ProviderNamespace: namespace, ProviderRecordID: mbid,
	}, reuse)
}

func (c *Client) call(ctx context.Context, method string, values url.Values, payload providers.Payload, reuse time.Duration) (providers.Payload, error) {
	key := c.apiKey
	if key == "" {
		key = c.config.APIKey
	}
	requestURL, err := url.Parse(c.config.BaseURL)
	if err != nil {
		return providers.Payload{}, fmt.Errorf("build Last.fm URL: %w", err)
	}
	values.Set("method", method)
	values.Set("format", "json")
	if key != "" {
		values.Set("api_key", key)
	}
	requestURL.RawQuery = values.Encode()
	safeValues := cloneValues(values)
	safeValues.Del("api_key")
	payload.RequestKey = safeValues.Encode()
	request, err := http.NewRequest(http.MethodGet, requestURL.String(), nil)
	if err != nil {
		return providers.Payload{}, fmt.Errorf("build Last.fm request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	return c.http.DoClassified(ctx, request, payload, func() error {
		if key == "" {
			return fmt.Errorf("Last.fm requires X-Heya-LastFM-API-Key or HEYA_METADATA_LASTFM_API_KEY")
		}
		return c.gate.Wait(ctx)
	}, classify(reuse))
}

func cloneValues(source url.Values) url.Values {
	result := url.Values{}
	for key, values := range source {
		result[key] = append([]string(nil), values...)
	}
	return result
}

func classify(reuse time.Duration) func(*providers.Payload) {
	return func(payload *providers.Payload) {
		if payload.StatusCode != http.StatusOK {
			return
		}
		var result struct {
			Error   int    `json:"error"`
			Message string `json:"message"`
		}
		if json.Unmarshal(payload.Body, &result) != nil {
			zero := time.Duration(0)
			payload.ReuseDurationOverride = &zero
			return
		}
		if result.Error != 0 {
			duration := time.Duration(0)
			if result.Error == 6 {
				duration = time.Hour
			}
			payload.ReuseDurationOverride = &duration
			return
		}
		payload.ReuseDurationOverride = &reuse
	}
}

func pagination(limit, page, defaultLimit int) (int, int) {
	if limit < 1 || limit > 1000 {
		limit = defaultLimit
	}
	if page < 1 {
		page = 1
	}
	return limit, page
}

package musicbrainz

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

var mbidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

var includes = map[string]string{
	"artist":        "aliases+annotation+artist-rels+genres+tags+url-rels",
	"release_group": "aliases+annotation+artist-credits+genres+ratings+releases+tags+url-rels",
	"release":       "artist-credits+artist-rels+discids+genres+isrcs+labels+media+recordings+recording-level-rels+release-groups+tags+url-rels+work-rels",
	"recording":     "artist-credits+artist-rels+genres+isrcs+ratings+releases+tags+url-rels+work-rels",
}

type Client struct {
	config config.MusicBrainzConfig
	http   *providers.HTTPClient
	gate   *providers.RequestGate
}

func New(config config.MusicBrainzConfig) *Client {
	return newClient(config, providers.NewHTTPClient(30*time.Second))
}

func NewCached(config config.MusicBrainzConfig, resolver providers.PayloadResolver) *Client {
	return newClient(config, providers.NewCachedHTTPClient(30*time.Second, resolver))
}

func newClient(config config.MusicBrainzConfig, httpClient *providers.HTTPClient) *Client {
	return &Client{
		config: config,
		http:   httpClient,
		gate:   providers.SharedRequestGate("musicbrainz:"+strings.TrimRight(config.BaseURL, "/"), config.RequestsPerSecond),
	}
}

func (c *Client) Capability() providers.Capability {
	return providers.Capability{
		Provider: "musicbrainz", EntityKind: "music_source",
		RawRetention:  providers.RetentionPolicy{Class: "provider_raw_48h", Duration: 48 * time.Hour, ObjectPrefix: "ephemeral/48h"},
		ResponseCache: providers.ResponseCachePolicy{ReuseDuration: 12 * time.Hour, NegativeDuration: time.Hour, RedisBodyDuration: time.Hour, MaxRedisBodyBytes: 4 * 1024 * 1024},
		AcceptedIdentifiers: []providers.Identifier{
			{Provider: "musicbrainz", Namespace: "artist"},
			{Provider: "musicbrainz", Namespace: "release_group"},
			{Provider: "musicbrainz", Namespace: "release"},
			{Provider: "musicbrainz", Namespace: "recording"},
		},
		Provides: []providers.Scope{
			providers.ScopeIdentity, providers.ScopeTitles, providers.ScopeDescriptions,
			providers.ScopeClassification, providers.ScopeReleases, providers.ScopeRatings,
		},
	}
}

func (c *Client) Collect(ctx context.Context, identifier providers.Identifier) ([]providers.Payload, error) {
	if identifier.Provider != "musicbrainz" {
		return nil, fmt.Errorf("MusicBrainz collector requires a MusicBrainz identifier")
	}
	inc, ok := includes[identifier.Namespace]
	if !ok {
		return nil, fmt.Errorf("unsupported MusicBrainz namespace %q", identifier.Namespace)
	}
	value := strings.ToLower(strings.TrimSpace(identifier.Value))
	if !mbidPattern.MatchString(value) {
		return nil, fmt.Errorf("MusicBrainz %s collector requires a valid MBID", identifier.Namespace)
	}
	resource := strings.ReplaceAll(identifier.Namespace, "_", "-")
	requestURL, err := url.Parse(strings.TrimRight(c.config.BaseURL, "/") + "/" + resource + "/" + url.PathEscape(value))
	if err != nil {
		return nil, fmt.Errorf("build MusicBrainz URL: %w", err)
	}
	query := requestURL.Query()
	query.Set("fmt", "json")
	query.Set("inc", inc)
	requestURL.RawQuery = query.Encode()
	payload, err := c.get(ctx, requestURL, providers.Payload{
		Provider: "musicbrainz", ProviderNamespace: identifier.Namespace, ProviderRecordID: value,
		RequestKey: resource + "/" + value + "?fmt=json&inc=" + inc,
	}, classifyReuse(value))
	if err != nil {
		return nil, err
	}
	return []providers.Payload{payload}, nil
}

// Search collects one page of MusicBrainz's Lucene search results. The query
// is source evidence, not an identity claim; callers must disambiguate hits
// before feeding an MBID back through Collect.
func (c *Client) Search(ctx context.Context, namespace, query string, limit, offset int) (providers.Payload, error) {
	resource, ok := searchResource(namespace)
	if !ok {
		return providers.Payload{}, fmt.Errorf("unsupported MusicBrainz search namespace %q", namespace)
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return providers.Payload{}, fmt.Errorf("MusicBrainz search query must not be empty")
	}
	if limit < 1 || limit > 100 {
		limit = 25
	}
	if offset < 0 {
		offset = 0
	}
	requestURL, err := url.Parse(strings.TrimRight(c.config.BaseURL, "/") + "/" + resource + "/")
	if err != nil {
		return providers.Payload{}, fmt.Errorf("build MusicBrainz search URL: %w", err)
	}
	values := requestURL.Query()
	values.Set("fmt", "json")
	values.Set("limit", fmt.Sprintf("%d", limit))
	values.Set("offset", fmt.Sprintf("%d", offset))
	values.Set("query", query)
	requestURL.RawQuery = values.Encode()
	payload := providers.Payload{
		Provider: "musicbrainz", ProviderNamespace: namespace + "_search", ProviderRecordID: query,
		RequestKey: resource + "/?" + values.Encode(),
	}
	return c.get(ctx, requestURL, payload, classifySearch(searchListField(namespace)))
}

// BrowseReleaseGroups collects one complete, explicitly paged slice of an
// artist's release-group catalog. Offsets advance by the number returned, not
// by a presumed fixed page size.
func (c *Client) BrowseReleaseGroups(ctx context.Context, artistMBID string, limit, offset int) (providers.Payload, error) {
	artistMBID = strings.ToLower(strings.TrimSpace(artistMBID))
	if !mbidPattern.MatchString(artistMBID) {
		return providers.Payload{}, fmt.Errorf("MusicBrainz release-group browse requires a valid artist MBID")
	}
	if limit < 1 || limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	requestURL, err := url.Parse(strings.TrimRight(c.config.BaseURL, "/") + "/release-group")
	if err != nil {
		return providers.Payload{}, fmt.Errorf("build MusicBrainz browse URL: %w", err)
	}
	values := requestURL.Query()
	values.Set("artist", artistMBID)
	values.Set("fmt", "json")
	values.Set("inc", "aliases+artist-credits+genres+tags")
	values.Set("limit", fmt.Sprintf("%d", limit))
	values.Set("offset", fmt.Sprintf("%d", offset))
	requestURL.RawQuery = values.Encode()
	payload := providers.Payload{
		Provider: "musicbrainz", ProviderNamespace: "artist_release_groups", ProviderRecordID: artistMBID,
		RequestKey: "release-group?" + values.Encode(),
	}
	return c.get(ctx, requestURL, payload, classifySearch("release-groups"))
}

func (c *Client) get(ctx context.Context, requestURL *url.URL, payload providers.Payload, classify func(*providers.Payload)) (providers.Payload, error) {
	request, err := http.NewRequest(http.MethodGet, requestURL.String(), nil)
	if err != nil {
		return providers.Payload{}, fmt.Errorf("build MusicBrainz request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	return c.http.DoPrepared(ctx, request, payload, func(request *http.Request) error {
		if strings.TrimSpace(c.config.UserAgent) == "" {
			return fmt.Errorf("MusicBrainz requires HEYA_METADATA_MUSICBRAINZ_USER_AGENT")
		}
		if err := c.gate.Wait(ctx); err != nil {
			return err
		}
		request.Header.Set("User-Agent", c.config.UserAgent)
		return nil
	}, classify)
}

func searchResource(namespace string) (string, bool) {
	_, ok := includes[namespace]
	return strings.ReplaceAll(namespace, "_", "-"), ok
}

func searchListField(namespace string) string {
	if namespace == "release_group" {
		return "release-groups"
	}
	return strings.ReplaceAll(namespace, "_", "-") + "s"
}

func classifyReuse(expectedID string) func(*providers.Payload) {
	return func(payload *providers.Payload) {
		if payload.StatusCode != http.StatusOK {
			return
		}
		var identity struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(payload.Body, &identity) != nil || !strings.EqualFold(identity.ID, expectedID) {
			zero := time.Duration(0)
			payload.ReuseDurationOverride = &zero
		}
	}
}

func classifySearch(listField string) func(*providers.Payload) {
	return func(payload *providers.Payload) {
		if payload.StatusCode != http.StatusOK {
			return
		}
		var result map[string]json.RawMessage
		if json.Unmarshal(payload.Body, &result) != nil || result[listField] == nil {
			zero := time.Duration(0)
			payload.ReuseDurationOverride = &zero
			return
		}
		sixHours := 6 * time.Hour
		payload.ReuseDurationOverride = &sixHours
	}
}

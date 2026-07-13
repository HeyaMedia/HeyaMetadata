package tmdb

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

const appendedMovieScopes = "credits,external_ids,keywords,release_dates,videos,recommendations,images,alternative_titles,translations"
const appendedTVScopes = "aggregate_credits,external_ids,keywords,content_ratings,videos,recommendations,images,alternative_titles,translations"
const appendedPersonScopes = "combined_credits,external_ids,images,translations"

type Client struct {
	config config.TMDBConfig
	apiKey string
	http   *providers.HTTPClient
}

// FindTVByIMDb resolves an explicit IMDb identifier through TMDB's external-ID
// index. The returned payload is evidence; CollectTV performs the detail fetch.
func (c *Client) FindTVByIMDb(ctx context.Context, imdbID string) (providers.Payload, error) {
	imdbID = strings.TrimSpace(imdbID)
	if !strings.HasPrefix(imdbID, "tt") {
		return providers.Payload{}, fmt.Errorf("TMDB TV lookup requires an IMDb title ID")
	}
	return c.get(ctx, "find/"+url.PathEscape(imdbID), url.Values{
		"external_source": {"imdb_id"},
		"language":        {c.config.Language},
	}, providers.Payload{Provider: "tmdb", ProviderNamespace: "tv_external_lookup", ProviderRecordID: imdbID, RequestKey: "find/" + imdbID + "?external_source=imdb_id&language=" + c.config.Language})
}

// CollectTV fetches a TV detail document plus every season document so the
// canonical mixer receives complete episode data rather than only season shells.
func (c *Client) CollectTV(ctx context.Context, identifier providers.Identifier) ([]providers.Payload, error) {
	if identifier.Provider != "tmdb" || identifier.Namespace != "tv" {
		return nil, fmt.Errorf("TMDB TV collector cannot accept %s.%s", identifier.Provider, identifier.Namespace)
	}
	id, err := strconv.ParseInt(identifier.Value, 10, 64)
	if err != nil || id < 1 {
		return nil, fmt.Errorf("invalid TMDB TV ID %q", identifier.Value)
	}
	detail, err := c.get(ctx, fmt.Sprintf("tv/%d", id), url.Values{
		"append_to_response":     {appendedTVScopes},
		"include_image_language": {languageFromLocale(c.config.Language) + ",en,null"},
		"language":               {c.config.Language},
	}, providers.Payload{Provider: "tmdb", ProviderNamespace: "tv", ProviderRecordID: identifier.Value, RequestKey: fmt.Sprintf("tv/%d?append=%s&language=%s", id, appendedTVScopes, c.config.Language)})
	if err != nil || detail.StatusCode != http.StatusOK {
		return []providers.Payload{detail}, err
	}
	var envelope struct {
		Seasons []struct {
			SeasonNumber int `json:"season_number"`
		} `json:"seasons"`
	}
	if err := json.Unmarshal(detail.Body, &envelope); err != nil {
		return nil, fmt.Errorf("decode TMDB TV envelope: %w", err)
	}
	payloads := []providers.Payload{detail}
	for _, season := range envelope.Seasons {
		// Season zero can contain hundreds of clips, featurettes, and trailers.
		// Those are supplemental assets, not canonical TV episodes.
		if season.SeasonNumber == 0 {
			continue
		}
		payload, err := c.get(ctx, fmt.Sprintf("tv/%d/season/%d", id, season.SeasonNumber), url.Values{"language": {c.config.Language}}, providers.Payload{
			Provider: "tmdb", ProviderNamespace: "tv_season", ProviderRecordID: fmt.Sprintf("%d:%d", id, season.SeasonNumber), RequestKey: fmt.Sprintf("tv/%d/season/%d?language=%s", id, season.SeasonNumber, c.config.Language),
		})
		if err != nil {
			return payloads, err
		}
		payloads = append(payloads, payload)
	}
	return payloads, nil
}

func PersonCapability() providers.Capability {
	return providers.Capability{
		Provider: "tmdb", EntityKind: "person",
		RawRetention:        providers.RetentionPolicy{Class: "provider_raw_48h", Duration: 48 * time.Hour, ObjectPrefix: "ephemeral/48h"},
		ResponseCache:       providers.ResponseCachePolicy{ReuseDuration: 24 * time.Hour, NegativeDuration: time.Hour, RedisBodyDuration: time.Hour, MaxRedisBodyBytes: 2 * 1024 * 1024},
		AcceptedIdentifiers: []providers.Identifier{{Provider: "tmdb", Namespace: "person"}},
		Provides:            []providers.Scope{providers.ScopeIdentity, providers.ScopeTitles, providers.ScopeDescriptions, providers.ScopeCredits, providers.ScopeArtwork},
	}
}

func (c *Client) CollectPerson(ctx context.Context, identifier providers.Identifier) ([]providers.Payload, error) {
	if identifier.Provider != "tmdb" || identifier.Namespace != "person" {
		return nil, fmt.Errorf("TMDB person collector cannot accept %s.%s", identifier.Provider, identifier.Namespace)
	}
	id, err := strconv.ParseInt(identifier.Value, 10, 64)
	if err != nil || id < 1 {
		return nil, fmt.Errorf("invalid TMDB person ID %q", identifier.Value)
	}
	payload, err := c.get(ctx, fmt.Sprintf("person/%d", id), url.Values{
		"append_to_response":     {appendedPersonScopes},
		"include_image_language": {languageFromLocale(c.config.Language) + ",en,null"},
		"language":               {c.config.Language},
	}, providers.Payload{Provider: "tmdb", ProviderNamespace: "person", ProviderRecordID: identifier.Value, RequestKey: fmt.Sprintf("person/%d?append=%s&language=%s", id, appendedPersonScopes, c.config.Language)})
	return []providers.Payload{payload}, err
}

func New(config config.TMDBConfig) *Client {
	return &Client{config: config, http: providers.NewHTTPClient(30 * time.Second)}
}

func NewCached(config config.TMDBConfig, resolver providers.PayloadResolver, apiKey string) *Client {
	return &Client{config: config, apiKey: apiKey, http: providers.NewCachedHTTPClient(30*time.Second, resolver)}
}

func (c *Client) Capability() providers.Capability {
	return providers.Capability{
		Provider: "tmdb", EntityKind: "movie",
		RawRetention: providers.RetentionPolicy{
			Class: "provider_raw_48h", Duration: 48 * time.Hour, ObjectPrefix: "ephemeral/48h",
		},
		ResponseCache: providers.ResponseCachePolicy{
			ReuseDuration: 48 * time.Hour, NegativeDuration: time.Hour,
			RedisBodyDuration: time.Hour, MaxRedisBodyBytes: 1024 * 1024,
		},
		AcceptedIdentifiers: []providers.Identifier{{Provider: "tmdb", Namespace: "movie"}},
		Provides: []providers.Scope{
			providers.ScopeIdentity, providers.ScopeTitles, providers.ScopeDescriptions,
			providers.ScopeClassification, providers.ScopeReleases, providers.ScopeRatings,
			providers.ScopeCredits, providers.ScopeArtwork, providers.ScopeCollections,
			providers.ScopeRecommendations,
		},
	}
}

func (c *Client) Collect(ctx context.Context, identifier providers.Identifier) ([]providers.Payload, error) {
	if identifier.Provider != "tmdb" || identifier.Namespace != "movie" {
		return nil, fmt.Errorf("TMDB movie collector cannot accept %s.%s", identifier.Provider, identifier.Namespace)
	}
	id, err := strconv.ParseInt(identifier.Value, 10, 64)
	if err != nil || id < 1 {
		return nil, fmt.Errorf("invalid TMDB movie ID %q", identifier.Value)
	}

	detail, err := c.get(ctx, fmt.Sprintf("movie/%d", id), url.Values{
		"append_to_response":     {appendedMovieScopes},
		"include_image_language": {languageFromLocale(c.config.Language) + ",en,null"},
		"language":               {c.config.Language},
	}, providers.Payload{
		Provider: "tmdb", ProviderNamespace: "movie", ProviderRecordID: strconv.FormatInt(id, 10),
		RequestKey: fmt.Sprintf("movie/%d?append=%s&language=%s", id, appendedMovieScopes, c.config.Language),
	})
	if err != nil {
		return nil, err
	}
	payloads := []providers.Payload{detail}
	if detail.StatusCode != http.StatusOK {
		return payloads, nil
	}
	var envelope struct {
		Collection *struct {
			ID int64 `json:"id"`
		} `json:"belongs_to_collection"`
	}
	if err := json.Unmarshal(detail.Body, &envelope); err != nil {
		return payloads, fmt.Errorf("decode TMDB movie envelope: %w", err)
	}
	if envelope.Collection != nil && envelope.Collection.ID > 0 {
		collection, err := c.get(ctx, fmt.Sprintf("collection/%d", envelope.Collection.ID), url.Values{
			"language": {c.config.Language},
		}, providers.Payload{
			Provider: "tmdb", ProviderNamespace: "collection",
			ProviderRecordID: strconv.FormatInt(envelope.Collection.ID, 10),
			RequestKey:       fmt.Sprintf("collection/%d?language=%s", envelope.Collection.ID, c.config.Language),
		})
		if err != nil {
			return payloads, err
		}
		payloads = append(payloads, collection)
	}
	return payloads, nil
}

// SearchMovies returns one explicitly paged set of identity candidates. Search
// results are evidence only; callers must resolve the selected TMDB ID before
// it becomes a canonical identity claim.
func (c *Client) SearchMovies(ctx context.Context, query string, year, page int) (providers.Payload, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return providers.Payload{}, fmt.Errorf("TMDB movie search query must not be empty")
	}
	if page < 1 || page > 500 {
		page = 1
	}
	values := url.Values{
		"query":         {query},
		"page":          {strconv.Itoa(page)},
		"include_adult": {"false"},
		"language":      {c.config.Language},
	}
	if year >= 1800 && year <= 2200 {
		values.Set("year", strconv.Itoa(year))
	}
	payload := providers.Payload{
		Provider: "tmdb", ProviderNamespace: "movie_search", ProviderRecordID: query,
		RequestKey: "search/movie?" + values.Encode(),
	}
	return c.get(ctx, "search/movie", values, payload)
}

func (c *Client) get(ctx context.Context, endpoint string, query url.Values, payload providers.Payload) (providers.Payload, error) {
	base := strings.TrimRight(c.config.BaseURL, "/") + "/" + endpoint
	requestURL, err := url.Parse(base)
	if err != nil {
		return providers.Payload{}, fmt.Errorf("build TMDB URL: %w", err)
	}
	requestURL.RawQuery = query.Encode()
	if c.apiKey != "" {
		values := requestURL.Query()
		values.Set("api_key", c.apiKey)
		requestURL.RawQuery = values.Encode()
	}
	request, err := http.NewRequest(http.MethodGet, requestURL.String(), nil)
	if err != nil {
		return providers.Payload{}, fmt.Errorf("build TMDB request: %w", err)
	}
	if c.apiKey == "" && c.config.Token != "" {
		request.Header.Set("Authorization", "Bearer "+c.config.Token)
	}
	request.Header.Set("Accept", "application/json")
	return c.http.DoGuarded(ctx, request, payload, func() error {
		if c.apiKey == "" && c.config.Token == "" {
			return fmt.Errorf("TMDB requires X-Heya-TMDB-API-Key or HEYA_METADATA_TMDB_TOKEN")
		}
		return nil
	})
}

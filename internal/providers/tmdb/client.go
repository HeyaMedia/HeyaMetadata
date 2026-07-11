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

type Client struct {
	config config.TMDBConfig
	http   *providers.HTTPClient
}

func New(config config.TMDBConfig) *Client {
	return &Client{config: config, http: providers.NewHTTPClient(30 * time.Second)}
}

func (c *Client) Capability() providers.Capability {
	return providers.Capability{
		Provider: "tmdb", EntityKind: "movie",
		RawRetention:        48 * time.Hour,
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
	if c.config.Token == "" {
		return nil, fmt.Errorf("HEYA_METADATA_TMDB_TOKEN is not configured")
	}
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

func (c *Client) get(ctx context.Context, endpoint string, query url.Values, payload providers.Payload) (providers.Payload, error) {
	base := strings.TrimRight(c.config.BaseURL, "/") + "/" + endpoint
	requestURL, err := url.Parse(base)
	if err != nil {
		return providers.Payload{}, fmt.Errorf("build TMDB URL: %w", err)
	}
	requestURL.RawQuery = query.Encode()
	request, err := http.NewRequest(http.MethodGet, requestURL.String(), nil)
	if err != nil {
		return providers.Payload{}, fmt.Errorf("build TMDB request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+c.config.Token)
	request.Header.Set("Accept", "application/json")
	return c.http.Do(ctx, request, payload)
}

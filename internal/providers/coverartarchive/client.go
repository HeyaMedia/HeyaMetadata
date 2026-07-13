// Package coverartarchive collects image metadata indexed by MusicBrainz IDs.
package coverartarchive

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

type Client struct {
	config config.CoverArtArchiveConfig
	http   *providers.HTTPClient
	gate   *providers.RequestGate
}

func New(config config.CoverArtArchiveConfig) *Client {
	return newClient(config, providers.NewHTTPClient(30*time.Second))
}

func NewCached(config config.CoverArtArchiveConfig, resolver providers.PayloadResolver) *Client {
	return newClient(config, providers.NewCachedHTTPClient(30*time.Second, resolver))
}

func newClient(config config.CoverArtArchiveConfig, httpClient *providers.HTTPClient) *Client {
	return &Client{config: config, http: httpClient, gate: providers.SharedRequestGate("coverartarchive:"+strings.TrimRight(config.BaseURL, "/"), config.RequestsPerSecond)}
}

func (c *Client) Capability() providers.Capability {
	return providers.Capability{
		Provider: "coverartarchive", EntityKind: "music_artwork_source",
		RawRetention:        providers.RetentionPolicy{Class: "provider_raw_48h", Duration: 48 * time.Hour, ObjectPrefix: "ephemeral/48h"},
		ResponseCache:       providers.ResponseCachePolicy{ReuseDuration: 48 * time.Hour, NegativeDuration: 24 * time.Hour, RedisBodyDuration: time.Hour, MaxRedisBodyBytes: 2 * 1024 * 1024},
		AcceptedIdentifiers: []providers.Identifier{{Provider: "musicbrainz", Namespace: "release_group"}},
		Provides:            []providers.Scope{providers.ScopeIdentity, providers.ScopeArtwork},
	}
}

func (c *Client) Collect(ctx context.Context, identifier providers.Identifier) ([]providers.Payload, error) {
	if identifier.Provider != "musicbrainz" || identifier.Namespace != "release_group" {
		return nil, fmt.Errorf("Cover Art Archive collector requires a MusicBrainz release-group ID")
	}
	mbid := strings.ToLower(strings.TrimSpace(identifier.Value))
	if !mbidPattern.MatchString(mbid) {
		return nil, fmt.Errorf("Cover Art Archive collector requires a valid MusicBrainz release-group ID")
	}
	requestURL, err := url.Parse(strings.TrimRight(c.config.BaseURL, "/") + "/release-group/" + url.PathEscape(mbid))
	if err != nil {
		return nil, fmt.Errorf("build Cover Art Archive URL: %w", err)
	}
	request, err := http.NewRequest(http.MethodGet, requestURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("build Cover Art Archive request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	payload, err := c.http.DoPrepared(ctx, request, providers.Payload{Provider: "coverartarchive", ProviderNamespace: "release_group", ProviderRecordID: mbid, RequestKey: "release-group/" + mbid}, func(request *http.Request) error {
		if strings.TrimSpace(c.config.UserAgent) == "" {
			return fmt.Errorf("Cover Art Archive requires a user agent")
		}
		if err := c.gate.Wait(ctx); err != nil {
			return err
		}
		request.Header.Set("User-Agent", c.config.UserAgent)
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
	var response struct {
		Images []json.RawMessage `json:"images"`
	}
	if json.Unmarshal(payload.Body, &response) != nil {
		zero := time.Duration(0)
		payload.ReuseDurationOverride = &zero
	}
}

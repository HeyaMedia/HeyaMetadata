package anidb

import (
	"context"
	"encoding/xml"
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

var clientPattern = regexp.MustCompile(`^[a-z]{4,16}$`)

const (
	requestsPerSecond = 0.2
	bannedCooldown    = 30 * time.Minute
)

type Client struct {
	config config.AniDBConfig
	http   *providers.HTTPClient
	gate   *providers.RequestGate
}

func New(config config.AniDBConfig) *Client {
	return newClient(config, providers.NewHTTPClient(45*time.Second))
}

func NewCached(config config.AniDBConfig, resolver providers.PayloadResolver) *Client {
	return newClient(config, providers.NewCachedHTTPClient(45*time.Second, resolver))
}

func newClient(config config.AniDBConfig, client *providers.HTTPClient) *Client {
	return &Client{
		config: config,
		http:   client,
		// AniDB's two-second interval is an absolute short-term ceiling, not
		// a safe sustained batch rate. Keep the conservative five-second
		// cadence used by the original service.
		gate: providers.SharedRequestGate("anidb:"+config.BaseURL, requestsPerSecond),
	}
}

func (c *Client) Capability() providers.Capability {
	return providers.Capability{
		Provider: "anidb", EntityKind: "anime_source",
		RawRetention:        providers.RetentionPolicy{Class: "provider_raw_48h", Duration: 48 * time.Hour, ObjectPrefix: "ephemeral/48h"},
		ResponseCache:       providers.ResponseCachePolicy{ReuseDuration: 24 * time.Hour, NegativeDuration: time.Hour, RedisBodyDuration: time.Hour, MaxRedisBodyBytes: 8 * 1024 * 1024},
		AcceptedIdentifiers: []providers.Identifier{{Provider: "anidb", Namespace: "anime"}},
		Provides: []providers.Scope{
			providers.ScopeIdentity, providers.ScopeTitles, providers.ScopeDescriptions,
			providers.ScopeClassification, providers.ScopeReleases, providers.ScopeRatings,
			providers.ScopeCredits, providers.ScopeArtwork, providers.ScopeRecommendations,
		},
	}
}

func (c *Client) Collect(ctx context.Context, identifier providers.Identifier) ([]providers.Payload, error) {
	if identifier.Provider != "anidb" || identifier.Namespace != "anime" {
		return nil, fmt.Errorf("AniDB collector requires an anidb anime ID")
	}
	aid, err := strconv.ParseInt(identifier.Value, 10, 64)
	if err != nil || aid < 1 {
		return nil, fmt.Errorf("AniDB anime collector requires a positive AID")
	}
	requestURL, err := url.Parse(c.config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("build AniDB URL: %w", err)
	}
	values := requestURL.Query()
	values.Set("request", "anime")
	values.Set("client", strings.ToLower(c.config.Client))
	values.Set("clientver", strconv.Itoa(c.config.ClientVersion))
	values.Set("protover", "1")
	values.Set("aid", identifier.Value)
	requestURL.RawQuery = values.Encode()
	request, err := http.NewRequest(http.MethodGet, requestURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("build AniDB request: %w", err)
	}
	request.Header.Set("Accept", "application/xml, text/xml")
	request.Header.Set("User-Agent", c.config.UserAgent)
	payload, err := c.http.DoClassified(ctx, request, providers.Payload{
		Provider: "anidb", ProviderNamespace: "anime", ProviderRecordID: identifier.Value,
		RequestKey: "anime/" + identifier.Value + "?protover=1",
	}, func() error {
		if !clientPattern.MatchString(strings.ToLower(c.config.Client)) {
			return fmt.Errorf("AniDB requires a registered 4-16 letter HEYA_METADATA_ANIDB_CLIENT")
		}
		if c.gate.DeferredFor() > 0 {
			return &providers.StatusError{Provider: "anidb", StatusCode: http.StatusTooManyRequests}
		}
		return c.gate.Wait(ctx)
	}, classify(identifier.Value))
	if err == nil && !payload.FromCache && payload.StatusCode == http.StatusTooManyRequests {
		c.gate.Defer(bannedCooldown)
	}
	if err != nil {
		return nil, err
	}
	return []providers.Payload{payload}, nil
}

// Titles downloads AniDB's official daily title dump. It is the supported
// title-to-AID lookup surface; the detail HTTP API only accepts a known AID.
func (c *Client) Titles(ctx context.Context) (providers.Payload, error) {
	requestURL := strings.TrimSpace(c.config.TitlesURL)
	if requestURL == "" {
		return providers.Payload{}, fmt.Errorf("AniDB titles URL is not configured")
	}
	request, err := http.NewRequest(http.MethodGet, requestURL, nil)
	if err != nil {
		return providers.Payload{}, fmt.Errorf("build AniDB title dump request: %w", err)
	}
	request.Header.Set("Accept", "application/gzip, application/xml")
	request.Header.Set("User-Agent", c.config.UserAgent)
	payload := providers.Payload{Provider: "anidb", ProviderNamespace: "anime_title_dump", ProviderRecordID: "daily", RequestKey: "anime-titles.xml.gz"}
	return c.http.DoClassified(ctx, request, payload, nil, func(payload *providers.Payload) {
		if payload.StatusCode != http.StatusOK || len(payload.Body) == 0 {
			return
		}
		day := 24 * time.Hour
		payload.ReuseDurationOverride = &day
	})
}

func classify(expectedID string) func(*providers.Payload) {
	return func(payload *providers.Payload) {
		if payload.StatusCode != http.StatusOK {
			return
		}
		var envelope struct {
			XMLName xml.Name
			ID      string `xml:"id,attr"`
			Message string `xml:",chardata"`
		}
		if xml.Unmarshal(payload.Body, &envelope) != nil {
			zero := time.Duration(0)
			payload.ReuseDurationOverride = &zero
			return
		}
		if envelope.XMLName.Local == "error" {
			duration := time.Duration(0)
			message := strings.ToLower(envelope.Message)
			if strings.Contains(message, "not found") {
				// AniDB transports a missing anime as an HTTP 200 XML error
				// envelope. Convert that application-level result into the
				// provider status understood by discovery so a bad AID is
				// treated as unused evidence instead of a retryable parse error.
				payload.StatusCode = http.StatusNotFound
				duration = time.Hour
			} else if strings.Contains(message, "banned") {
				// The HTTP API also transports bans as HTTP 200. Expose a
				// rate-limit status to River and do not cache the error body.
				payload.StatusCode = http.StatusTooManyRequests
			}
			payload.ReuseDurationOverride = &duration
			return
		}
		if envelope.XMLName.Local != "anime" || envelope.ID != expectedID {
			zero := time.Duration(0)
			payload.ReuseDurationOverride = &zero
		}
	}
}

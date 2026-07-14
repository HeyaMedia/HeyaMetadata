package lrclib

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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

type Signature struct {
	TrackName  string
	ArtistName string
	AlbumName  string
	Duration   int
}

type Client struct {
	config config.LRCLIBConfig
	http   *providers.HTTPClient
	gate   *providers.RequestGate
}

const requestTimeout = 35 * time.Second

func New(config config.LRCLIBConfig) *Client {
	return newClient(config, providers.NewHTTPClient(requestTimeout))
}

func NewCached(config config.LRCLIBConfig, resolver providers.PayloadResolver) *Client {
	return newClient(config, providers.NewCachedHTTPClient(requestTimeout, resolver))
}

func newClient(config config.LRCLIBConfig, client *providers.HTTPClient) *Client {
	return &Client{config: config, http: client, gate: providers.SharedRequestGate("lrclib:"+strings.TrimRight(config.BaseURL, "/"), config.RequestsPerSecond)}
}

func (c *Client) Capability() providers.Capability {
	return providers.Capability{
		Provider: "lrclib", EntityKind: "recording_evidence",
		RawRetention:  providers.RetentionPolicy{Class: "provider_raw_48h", Duration: 48 * time.Hour, ObjectPrefix: "ephemeral/48h"},
		ResponseCache: providers.ResponseCachePolicy{ReuseDuration: 48 * time.Hour, NegativeDuration: 12 * time.Hour, RedisBodyDuration: time.Hour, MaxRedisBodyBytes: 2 * 1024 * 1024},
		Provides:      []providers.Scope{providers.ScopeLyrics},
	}
}

// Get calls LRCLIB's documented exact metadata endpoint. Even hits can take
// several seconds, so callers must keep this lookup on a background worker.
func (c *Client) Get(ctx context.Context, signature Signature) (providers.Payload, error) {
	return c.get(ctx, "/api/get", signature)
}

func (c *Client) get(ctx context.Context, path string, signature Signature) (providers.Payload, error) {
	signature.TrackName = strings.TrimSpace(signature.TrackName)
	signature.ArtistName = strings.TrimSpace(signature.ArtistName)
	signature.AlbumName = strings.TrimSpace(signature.AlbumName)
	if signature.TrackName == "" || signature.ArtistName == "" || signature.AlbumName == "" || signature.Duration < 1 {
		return providers.Payload{}, fmt.Errorf("LRCLIB exact lookup requires track, artist, album, and positive duration")
	}
	values := url.Values{
		"track_name":  {signature.TrackName},
		"artist_name": {signature.ArtistName},
		"album_name":  {signature.AlbumName},
		"duration":    {strconv.Itoa(signature.Duration)},
	}
	requestURL, err := url.Parse(strings.TrimRight(c.config.BaseURL, "/") + path)
	if err != nil {
		return providers.Payload{}, fmt.Errorf("build LRCLIB URL: %w", err)
	}
	requestURL.RawQuery = values.Encode()
	request, err := http.NewRequest(http.MethodGet, requestURL.String(), nil)
	if err != nil {
		return providers.Payload{}, fmt.Errorf("build LRCLIB request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", c.config.UserAgent)
	sum := sha256.Sum256([]byte(values.Encode()))
	payload := providers.Payload{Provider: "lrclib", ProviderNamespace: "lyrics_signature", ProviderRecordID: hex.EncodeToString(sum[:]), RequestKey: strings.TrimPrefix(path, "/api/") + "?" + values.Encode()}
	return c.http.DoPrepared(ctx, request, payload, func(*http.Request) error { return c.gate.Wait(ctx) }, classify)
}

func classify(payload *providers.Payload) {
	if payload.StatusCode != http.StatusOK {
		return
	}
	var value struct {
		ID int64 `json:"id"`
	}
	if json.Unmarshal(payload.Body, &value) != nil || value.ID < 1 {
		zero := time.Duration(0)
		payload.ReuseDurationOverride = &zero
	}
}

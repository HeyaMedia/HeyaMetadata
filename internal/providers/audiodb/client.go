package audiodb

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
	config config.AudioDBConfig
	http   *providers.HTTPClient
	gate   *providers.RequestGate
}

func New(config config.AudioDBConfig) *Client {
	return newClient(config, providers.NewHTTPClient(30*time.Second))
}

func NewCached(config config.AudioDBConfig, resolver providers.PayloadResolver) *Client {
	return newClient(config, providers.NewCachedHTTPClient(30*time.Second, resolver))
}

func newClient(config config.AudioDBConfig, client *providers.HTTPClient) *Client {
	return &Client{
		config: config,
		http:   client,
		gate:   providers.SharedRequestGate("audiodb:"+strings.TrimRight(config.BaseURL, "/"), config.RequestsPerSecond),
	}
}

func (c *Client) Capability() providers.Capability {
	return providers.Capability{
		Provider: "audiodb", EntityKind: "music_source",
		RawRetention:        providers.RetentionPolicy{Class: "provider_raw_48h", Duration: 48 * time.Hour, ObjectPrefix: "ephemeral/48h"},
		ResponseCache:       providers.ResponseCachePolicy{ReuseDuration: 24 * time.Hour, NegativeDuration: time.Hour, RedisBodyDuration: time.Hour, MaxRedisBodyBytes: 4 * 1024 * 1024},
		AcceptedIdentifiers: []providers.Identifier{{Provider: "musicbrainz", Namespace: "artist"}, {Provider: "musicbrainz", Namespace: "release_group"}},
		Provides: []providers.Scope{
			providers.ScopeIdentity, providers.ScopeDescriptions,
			providers.ScopeClassification, providers.ScopeRatings,
			providers.ScopeArtwork, providers.ScopeVideos,
		},
	}
}

func (c *Client) Collect(ctx context.Context, identifier providers.Identifier) ([]providers.Payload, error) {
	endpoints := map[string]string{"artist": "artist-mb.php", "release_group": "album-mb.php"}
	endpoint := endpoints[identifier.Namespace]
	if identifier.Provider != "musicbrainz" || endpoint == "" {
		return nil, fmt.Errorf("TheAudioDB collector requires a MusicBrainz artist or release group MBID")
	}
	mbid := strings.ToLower(strings.TrimSpace(identifier.Value))
	if !mbidPattern.MatchString(mbid) {
		return nil, fmt.Errorf("TheAudioDB collector requires a valid MusicBrainz MBID")
	}
	payload, err := c.get(ctx, endpoint, url.Values{"i": {mbid}}, providers.Payload{
		Provider: "audiodb", ProviderNamespace: identifier.Namespace, ProviderRecordID: mbid,
		RequestKey: strings.TrimSuffix(endpoint, ".php") + "/" + mbid,
	})
	if err != nil {
		return nil, err
	}
	return []providers.Payload{payload}, nil
}

// ArtistMusicVideos fetches mvid-mb.php: the artist's music videos with
// YouTube URLs, keyed by MusicBrainz artist ID.
func (c *Client) ArtistMusicVideos(ctx context.Context, mbid string) (providers.Payload, error) {
	mbid = strings.ToLower(strings.TrimSpace(mbid))
	if !mbidPattern.MatchString(mbid) {
		return providers.Payload{}, fmt.Errorf("TheAudioDB music videos require a valid MusicBrainz artist MBID")
	}
	return c.get(ctx, "mvid-mb.php", url.Values{"i": {mbid}}, providers.Payload{
		Provider: "audiodb", ProviderNamespace: "artist_music_videos", ProviderRecordID: mbid,
		RequestKey: "mvid-mb/" + mbid,
	})
}

// get keeps the API key out of RequestKey: TheAudioDB keys are path segments,
// so a raw URL would leak them into the shared cache fingerprint.
func (c *Client) get(ctx context.Context, endpoint string, values url.Values, payload providers.Payload) (providers.Payload, error) {
	requestURL, err := url.Parse(strings.TrimRight(c.config.BaseURL, "/") + "/" + url.PathEscape(c.config.APIKey) + "/" + endpoint)
	if err != nil {
		return providers.Payload{}, fmt.Errorf("build TheAudioDB URL: %w", err)
	}
	requestURL.RawQuery = values.Encode()
	request, err := http.NewRequest(http.MethodGet, requestURL.String(), nil)
	if err != nil {
		return providers.Payload{}, fmt.Errorf("build TheAudioDB request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	return c.http.DoClassified(ctx, request, payload, func() error {
		if strings.TrimSpace(c.config.APIKey) == "" {
			return fmt.Errorf("TheAudioDB requires HEYA_METADATA_AUDIODB_API_KEY")
		}
		return c.gate.Wait(ctx)
	}, classify)
}

// classify marks HTTP-200 "no such record" responses ({"artists": null} /
// {"album": null}) as briefly reusable and malformed bodies as non-reusable.
func classify(payload *providers.Payload) {
	if payload.StatusCode != http.StatusOK {
		return
	}
	var response struct {
		Artists []json.RawMessage `json:"artists"`
		Albums  []json.RawMessage `json:"album"`
	}
	if err := json.Unmarshal(payload.Body, &response); err != nil {
		zero := time.Duration(0)
		payload.ReuseDurationOverride = &zero
		return
	}
	if len(response.Artists) == 0 && len(response.Albums) == 0 {
		hour := time.Hour
		payload.ReuseDurationOverride = &hour
	}
}

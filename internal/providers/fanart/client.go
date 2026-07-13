package fanart

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

var mbidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

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
		Provider: "fanart", EntityKind: "artwork_source",
		RawRetention:        providers.RetentionPolicy{Class: "provider_raw_48h", Duration: 48 * time.Hour, ObjectPrefix: "ephemeral/48h"},
		ResponseCache:       providers.ResponseCachePolicy{ReuseDuration: 24 * time.Hour, NegativeDuration: time.Hour, RedisBodyDuration: time.Hour, MaxRedisBodyBytes: 4 * 1024 * 1024},
		AcceptedIdentifiers: []providers.Identifier{{Provider: "tmdb", Namespace: "movie"}, {Provider: "tvdb", Namespace: "series"}, {Provider: "musicbrainz", Namespace: "artist"}},
		Provides:            []providers.Scope{providers.ScopeIdentity, providers.ScopeArtwork},
	}
}

func (c *Client) Collect(ctx context.Context, identifier providers.Identifier) ([]providers.Payload, error) {
	providerNamespace := ""
	endpoint := ""
	switch {
	case identifier.Provider == "tmdb" && identifier.Namespace == "movie":
		providerNamespace = "movie"
		endpoint = "movies"
	case identifier.Provider == "tvdb" && identifier.Namespace == "series":
		providerNamespace = "series"
		endpoint = "tv"
	case identifier.Provider == "musicbrainz" && identifier.Namespace == "artist":
		providerNamespace = "artist"
		endpoint = "music"
	default:
		return nil, fmt.Errorf("Fanart.tv collector requires a TMDB movie ID, TVDB series ID, or MusicBrainz artist ID")
	}
	providerID := strings.ToLower(strings.TrimSpace(identifier.Value))
	if providerNamespace == "artist" {
		if !mbidPattern.MatchString(providerID) {
			return nil, fmt.Errorf("Fanart.tv music collector requires a valid MusicBrainz artist ID")
		}
	} else {
		numericID, err := strconv.ParseInt(providerID, 10, 64)
		if err != nil || numericID < 1 {
			return nil, fmt.Errorf("Fanart.tv collector requires a positive provider ID")
		}
		providerID = strconv.FormatInt(numericID, 10)
	}
	requestURL, err := url.Parse(strings.TrimRight(c.config.BaseURL, "/") + "/" + endpoint + "/" + providerID)
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
		Provider: "fanart", ProviderNamespace: providerNamespace, ProviderRecordID: providerID,
		RequestKey: endpoint + "/" + providerID,
	}, func() error {
		if c.config.APIKey == "" && c.clientKey == "" {
			return fmt.Errorf("Fanart.tv requires X-Heya-Fanart-API-Key or HEYA_METADATA_FANART_API_KEY")
		}
		return nil
	}, classifyReuse(providerNamespace))
	if err != nil {
		return nil, err
	}
	return []providers.Payload{payload}, nil
}

func classifyReuse(namespace string) func(*providers.Payload) {
	return func(payload *providers.Payload) {
		if payload.StatusCode != http.StatusOK {
			return
		}
		empty := false
		if namespace == "artist" {
			var response musicResponse
			if err := json.Unmarshal(payload.Body, &response); err != nil {
				zero := time.Duration(0)
				payload.ReuseDurationOverride = &zero
				return
			}
			empty = response.MBID == "" && musicImageCount(response) == 0
		} else if namespace == "series" {
			var response tvResponse
			if err := json.Unmarshal(payload.Body, &response); err != nil {
				zero := time.Duration(0)
				payload.ReuseDurationOverride = &zero
				return
			}
			empty = response.TVDBID == "" && tvImageCount(response) == 0
		} else {
			var response movieResponse
			if err := json.Unmarshal(payload.Body, &response); err != nil {
				zero := time.Duration(0)
				payload.ReuseDurationOverride = &zero
				return
			}
			empty = response.TMDBID == "" && response.IMDBID == "" && movieImageCount(response) == 0
		}
		if empty {
			hour := time.Hour
			payload.ReuseDurationOverride = &hour
		}
	}
}

func musicImageCount(response musicResponse) int {
	count := len(response.ArtistBackgrounds) + len(response.ArtistThumbs) + len(response.HDArtistLogos) + len(response.HDMusicLogos) + len(response.MusicLogos) + len(response.MusicBanners)
	for _, album := range response.Albums {
		count += len(album.AlbumCovers) + len(album.CDArt)
	}
	return count
}

func movieImageCount(response movieResponse) int {
	return len(response.MoviePosters) + len(response.MovieBackgrounds) + len(response.HDMovieLogos) +
		len(response.MovieLogos) + len(response.MovieBanners) + len(response.HDMovieClearArts) +
		len(response.MovieArts) + len(response.MovieThumbs) + len(response.MovieDiscs)
}

func tvImageCount(response tvResponse) int {
	return len(response.TVPosters) + len(response.ShowBackgrounds) + len(response.HDTVLogos) +
		len(response.ClearLogos) + len(response.TVBanners) + len(response.HDClearArts) +
		len(response.ClearArts) + len(response.TVThumbs) + len(response.CharacterArts) +
		len(response.SeasonPosters) + len(response.SeasonBanners) + len(response.SeasonThumbs)
}

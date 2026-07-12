// Package animelists provides an explicit AniDB/MAL/AniList/TVDB identity
// bridge. It never performs title matching.
package animelists

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/config"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers"
)

type Entry struct {
	AniDBID   int `json:"anidb_id"`
	MALID     int `json:"mal_id"`
	AniListID int `json:"anilist_id"`
	TVDBID    int `json:"tvdb_id"`
	Season    struct {
		TVDB *int `json:"tvdb"`
	} `json:"season"`
	EpisodeOffset struct {
		TVDB int `json:"tvdb"`
	} `json:"episode_offset"`
}

type Client struct {
	config config.AnimeListsConfig
	http   *providers.HTTPClient
}

func New(config config.AnimeListsConfig) *Client {
	return &Client{config: config, http: providers.NewHTTPClient(120 * time.Second)}
}
func NewCached(config config.AnimeListsConfig, resolver providers.PayloadResolver) *Client {
	return &Client{config: config, http: providers.NewCachedHTTPClient(120*time.Second, resolver)}
}
func (c *Client) Capability() providers.Capability {
	return providers.Capability{Provider: "anime_lists", EntityKind: "anime_mapping", RawRetention: providers.RetentionPolicy{Class: "provider_raw_48h", Duration: 48 * time.Hour, ObjectPrefix: "ephemeral/48h"}, ResponseCache: providers.ResponseCachePolicy{ReuseDuration: 48 * time.Hour, NegativeDuration: time.Hour, RedisBodyDuration: time.Hour, MaxRedisBodyBytes: 8 * 1024 * 1024}, AcceptedIdentifiers: []providers.Identifier{{Provider: "anidb", Namespace: "anime"}}, Provides: []providers.Scope{providers.ScopeIdentity}}
}
func (c *Client) Lookup(ctx context.Context, aid string) (providers.Payload, Entry, bool, error) {
	if n, err := strconv.Atoi(aid); err != nil || n < 1 {
		return providers.Payload{}, Entry{}, false, fmt.Errorf("anime-lists requires a positive AniDB ID")
	}
	req, err := http.NewRequest(http.MethodGet, strings.TrimSpace(c.config.URL), nil)
	if err != nil {
		return providers.Payload{}, Entry{}, false, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.config.UserAgent)
	payload, err := c.http.DoClassified(ctx, req, providers.Payload{Provider: "anime_lists", ProviderNamespace: "mapping_dump", ProviderRecordID: "weekly", RequestKey: "anime-list-mini.json"}, nil, func(p *providers.Payload) {
		if p.StatusCode == http.StatusOK {
			d := 48 * time.Hour
			p.ReuseDurationOverride = &d
		}
	})
	if err != nil {
		return payload, Entry{}, false, err
	}
	if payload.StatusCode != http.StatusOK {
		return payload, Entry{}, false, nil
	}
	var entries []Entry
	if err := json.Unmarshal(payload.Body, &entries); err != nil {
		return payload, Entry{}, false, fmt.Errorf("decode anime-lists mapping dump: %w", err)
	}
	want, _ := strconv.Atoi(aid)
	for _, entry := range entries {
		if entry.AniDBID == want {
			return payload, entry, true, nil
		}
	}
	return payload, Entry{}, false, nil
}

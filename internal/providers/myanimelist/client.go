package myanimelist

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

type Client struct {
	config   config.MyAnimeListConfig
	clientID string
	http     *providers.HTTPClient
	gate     *providers.RequestGate
}

func New(cfg config.MyAnimeListConfig, clientID string) *Client {
	return newClient(cfg, clientID, providers.NewHTTPClient(30*time.Second))
}
func NewCached(cfg config.MyAnimeListConfig, resolver providers.PayloadResolver, clientID string) *Client {
	return newClient(cfg, clientID, providers.NewCachedHTTPClient(30*time.Second, resolver))
}
func newClient(cfg config.MyAnimeListConfig, clientID string, client *providers.HTTPClient) *Client {
	if strings.TrimSpace(clientID) == "" {
		clientID = cfg.ClientID
	}
	return &Client{config: cfg, clientID: clientID, http: client, gate: providers.SharedRequestGate("myanimelist:"+strings.TrimRight(cfg.BaseURL, "/"), cfg.RequestsPerSecond)}
}
func (c *Client) Capability() providers.Capability {
	return providers.Capability{Provider: "myanimelist", EntityKind: "manga_source", RawRetention: providers.RetentionPolicy{Class: "provider_raw_48h", Duration: 48 * time.Hour, ObjectPrefix: "ephemeral/48h"}, ResponseCache: providers.ResponseCachePolicy{ReuseDuration: 12 * time.Hour, NegativeDuration: time.Hour, RedisBodyDuration: time.Hour, MaxRedisBodyBytes: 8 << 20}, AcceptedIdentifiers: []providers.Identifier{{Provider: "myanimelist", Namespace: "manga"}}, Provides: []providers.Scope{providers.ScopeIdentity, providers.ScopeTitles, providers.ScopeDescriptions, providers.ScopeClassification, providers.ScopeReleases, providers.ScopeRatings, providers.ScopeArtwork}}
}
func (c *Client) Collect(ctx context.Context, id providers.Identifier) ([]providers.Payload, error) {
	value := strings.TrimSpace(id.Value)
	if id.Provider != "myanimelist" || id.Namespace != "manga" {
		return nil, fmt.Errorf("MyAnimeList collector requires myanimelist:manga identifier")
	}
	if _, err := strconv.ParseInt(value, 10, 64); err != nil {
		return nil, fmt.Errorf("invalid MyAnimeList manga ID")
	}
	u, err := url.Parse(strings.TrimRight(c.config.BaseURL, "/") + "/manga/" + value)
	if err != nil {
		return nil, err
	}
	u.RawQuery = url.Values{"fields": {"id,title,main_picture,alternative_titles,start_date,end_date,synopsis,mean,rank,popularity,num_list_users,num_scoring_users,nsfw,genres,created_at,updated_at,media_type,status,num_volumes,num_chapters,authors{first_name,last_name}"}}.Encode()
	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	payload := providers.Payload{Provider: "myanimelist", ProviderNamespace: "manga", ProviderRecordID: value, RequestKey: "manga/" + value + "?" + u.RawQuery}
	p, err := c.http.DoPrepared(ctx, req, payload, func(r *http.Request) error {
		if strings.TrimSpace(c.clientID) == "" {
			return fmt.Errorf("MyAnimeList client ID is not configured")
		}
		if err := c.gate.Wait(ctx); err != nil {
			return err
		}
		r.Header.Set("X-MAL-CLIENT-ID", c.clientID)
		return nil
	}, func(p *providers.Payload) {
		if p.StatusCode == http.StatusOK {
			var v any
			if json.Unmarshal(p.Body, &v) == nil {
				d := 12 * time.Hour
				p.ReuseDurationOverride = &d
			}
		}
	})
	if err != nil {
		return nil, err
	}
	return []providers.Payload{p}, nil
}

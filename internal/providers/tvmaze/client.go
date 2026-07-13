package tvmaze

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

var lookupKinds = map[string]string{
	"imdb:title":  "imdb",
	"tvdb:series": "thetvdb",
	"tvrage:show": "tvrage",
}

type Client struct {
	config config.TVMazeConfig
	http   *providers.HTTPClient
	gate   *providers.RequestGate
}

func New(config config.TVMazeConfig) *Client {
	return newClient(config, providers.NewHTTPClient(30*time.Second))
}

func NewCached(config config.TVMazeConfig, resolver providers.PayloadResolver) *Client {
	return newClient(config, providers.NewCachedHTTPClient(30*time.Second, resolver))
}

func newClient(config config.TVMazeConfig, client *providers.HTTPClient) *Client {
	return &Client{
		config: config,
		http:   client,
		gate:   providers.SharedRequestGate("tvmaze:"+strings.TrimRight(config.BaseURL, "/"), config.RequestsPerSecond),
	}
}

func (c *Client) Capability() providers.Capability {
	return providers.Capability{
		Provider: "tvmaze", EntityKind: "television_source",
		RawRetention:  providers.RetentionPolicy{Class: "provider_raw_48h", Duration: 48 * time.Hour, ObjectPrefix: "ephemeral/48h"},
		ResponseCache: providers.ResponseCachePolicy{ReuseDuration: 6 * time.Hour, NegativeDuration: time.Hour, RedisBodyDuration: time.Hour, MaxRedisBodyBytes: 16 * 1024 * 1024},
		AcceptedIdentifiers: []providers.Identifier{
			{Provider: "tvmaze", Namespace: "show"},
			{Provider: "tvmaze", Namespace: "person"},
			{Provider: "imdb", Namespace: "title"},
			{Provider: "tvdb", Namespace: "series"},
			{Provider: "tvrage", Namespace: "show"},
		},
		Provides: []providers.Scope{
			providers.ScopeIdentity, providers.ScopeTitles, providers.ScopeDescriptions,
			providers.ScopeClassification, providers.ScopeReleases, providers.ScopeRatings,
			providers.ScopeCredits, providers.ScopeArtwork,
		},
	}
}

func (c *Client) Collect(ctx context.Context, identifier providers.Identifier) ([]providers.Payload, error) {
	if identifier.Provider == "tvmaze" && identifier.Namespace == "person" {
		if !positiveID(identifier.Value) {
			return nil, fmt.Errorf("TVMaze person collector requires a positive ID")
		}
		payload, err := c.get(ctx, "/people/"+identifier.Value, nil, providers.Payload{
			Provider: "tvmaze", ProviderNamespace: "person", ProviderRecordID: identifier.Value,
		}, 24*time.Hour, classifyObjectID(identifier.Value))
		if err != nil {
			return nil, err
		}
		payloads := []providers.Payload{payload}
		if payload.StatusCode != http.StatusOK {
			return payloads, nil
		}
		for _, creditType := range []string{"castcredits", "crewcredits"} {
			credits, err := c.get(ctx, "/people/"+identifier.Value+"/"+creditType, url.Values{"embed": {"show"}}, providers.Payload{
				Provider: "tvmaze", ProviderNamespace: "person_" + creditType, ProviderRecordID: identifier.Value,
			}, 24*time.Hour, classifyArray)
			if err != nil {
				return payloads, err
			}
			payloads = append(payloads, credits)
		}
		return payloads, nil
	}
	if identifier.Provider == "tvmaze" && identifier.Namespace == "show" {
		return c.collectShow(ctx, identifier.Value, nil)
	}
	kind := lookupKinds[identifier.Provider+":"+identifier.Namespace]
	if kind == "" || strings.TrimSpace(identifier.Value) == "" {
		return nil, fmt.Errorf("TVMaze collector requires a TVMaze show/person or supported external show ID")
	}
	lookup, err := c.get(ctx, "/lookup/shows", url.Values{kind: {identifier.Value}}, providers.Payload{
		Provider: "tvmaze", ProviderNamespace: "lookup_" + kind, ProviderRecordID: identifier.Value,
	}, 6*time.Hour, classifyObject)
	if err != nil {
		return nil, err
	}
	payloads := []providers.Payload{lookup}
	if lookup.StatusCode != http.StatusOK {
		return payloads, nil
	}
	var show struct {
		ID int64 `json:"id"`
	}
	if json.Unmarshal(lookup.Body, &show) != nil || show.ID < 1 {
		return payloads, nil
	}
	return c.collectShow(ctx, strconv.FormatInt(show.ID, 10), payloads)
}

func (c *Client) collectShow(ctx context.Context, id string, payloads []providers.Payload) ([]providers.Payload, error) {
	if !positiveID(id) {
		return nil, fmt.Errorf("TVMaze show collector requires a positive ID")
	}
	detail, err := c.get(ctx, "/shows/"+id, embedValues("cast", "crew", "seasons", "episodes", "images", "akas"), providers.Payload{
		Provider: "tvmaze", ProviderNamespace: "show", ProviderRecordID: id,
	}, 6*time.Hour, classifyObjectID(id))
	if err != nil {
		return payloads, err
	}
	return append(payloads, detail), nil
}

func (c *Client) Search(ctx context.Context, namespace, query string) (providers.Payload, error) {
	query = strings.TrimSpace(query)
	if query == "" || (namespace != "show" && namespace != "person") {
		return providers.Payload{}, fmt.Errorf("TVMaze search requires show or person and a query")
	}
	path := "/search/shows"
	if namespace == "person" {
		path = "/search/people"
	}
	return c.get(ctx, path, url.Values{"q": {query}}, providers.Payload{
		Provider: "tvmaze", ProviderNamespace: namespace + "_search", ProviderRecordID: query,
	}, 6*time.Hour, classifyArray)
}

func (c *Client) get(ctx context.Context, path string, values url.Values, payload providers.Payload, reuse time.Duration, classify func(*providers.Payload)) (providers.Payload, error) {
	requestURL, err := url.Parse(strings.TrimRight(c.config.BaseURL, "/") + path)
	if err != nil {
		return providers.Payload{}, fmt.Errorf("build TVMaze URL: %w", err)
	}
	if values != nil {
		requestURL.RawQuery = values.Encode()
	}
	payload.RequestKey = strings.TrimPrefix(path, "/")
	if requestURL.RawQuery != "" {
		payload.RequestKey += "?" + requestURL.RawQuery
	}
	request, err := http.NewRequest(http.MethodGet, requestURL.String(), nil)
	if err != nil {
		return providers.Payload{}, fmt.Errorf("build TVMaze request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	return c.http.DoPrepared(ctx, request, payload, func(*http.Request) error {
		return c.gate.Wait(ctx)
	}, func(payload *providers.Payload) {
		classify(payload)
		if payload.ReuseDurationOverride == nil && payload.StatusCode == http.StatusOK {
			payload.ReuseDurationOverride = &reuse
		}
	})
}

func embedValues(names ...string) url.Values {
	values := url.Values{}
	for _, name := range names {
		values.Add("embed[]", name)
	}
	return values
}

func classifyObject(payload *providers.Payload) {
	if payload.StatusCode != http.StatusOK {
		return
	}
	var value map[string]json.RawMessage
	if json.Unmarshal(payload.Body, &value) != nil || value["id"] == nil {
		zero := time.Duration(0)
		payload.ReuseDurationOverride = &zero
	}
}

func classifyObjectID(expected string) func(*providers.Payload) {
	return func(payload *providers.Payload) {
		classifyObject(payload)
		if payload.ReuseDurationOverride != nil || payload.StatusCode != http.StatusOK {
			return
		}
		var value struct {
			ID int64 `json:"id"`
		}
		if json.Unmarshal(payload.Body, &value) != nil || strconv.FormatInt(value.ID, 10) != expected {
			zero := time.Duration(0)
			payload.ReuseDurationOverride = &zero
		}
	}
}

func classifyArray(payload *providers.Payload) {
	if payload.StatusCode != http.StatusOK {
		return
	}
	var values []json.RawMessage
	if json.Unmarshal(payload.Body, &values) != nil {
		zero := time.Duration(0)
		payload.ReuseDurationOverride = &zero
		return
	}
	if len(values) == 0 {
		hour := time.Hour
		payload.ReuseDurationOverride = &hour
	}
}

func positiveID(value string) bool {
	id, err := strconv.ParseInt(value, 10, 64)
	return err == nil && id > 0
}

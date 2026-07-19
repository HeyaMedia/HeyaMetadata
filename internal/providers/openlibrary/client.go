package openlibrary

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

var keyPattern = regexp.MustCompile(`^OL[0-9]+[WMA]$`)

// CanonicalKey accepts the key shapes Open Library emits in API payloads and
// URLs while keeping one opaque identity for persistence and routing.
func CanonicalKey(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if parsed, err := url.Parse(value); err == nil && parsed.IsAbs() {
		if !strings.EqualFold(parsed.Hostname(), "openlibrary.org") {
			return "", false
		}
		value = parsed.Path
	}
	value = strings.Trim(value, "/")
	parts := strings.Split(value, "/")
	expectedSuffix := ""
	if len(parts) == 2 {
		switch strings.ToLower(parts[0]) {
		case "works":
			expectedSuffix = "W"
		case "books":
			expectedSuffix = "M"
		case "authors":
			expectedSuffix = "A"
		default:
			return "", false
		}
		value = parts[1]
	} else if len(parts) != 1 {
		return "", false
	}
	value = strings.ToUpper(strings.TrimSpace(value))
	if !keyPattern.MatchString(value) || expectedSuffix != "" && !strings.HasSuffix(value, expectedSuffix) {
		return "", false
	}
	return value, true
}

type Client struct {
	config config.OpenLibraryConfig
	http   *providers.HTTPClient
	gate   *providers.RequestGate
}

func New(config config.OpenLibraryConfig) *Client {
	return newClient(config, providers.NewHTTPClient(30*time.Second))
}
func NewCached(config config.OpenLibraryConfig, resolver providers.PayloadResolver) *Client {
	return newClient(config, providers.NewCachedHTTPClient(30*time.Second, resolver))
}
func newClient(config config.OpenLibraryConfig, httpClient *providers.HTTPClient) *Client {
	return &Client{config: config, http: httpClient, gate: providers.SharedRequestGate("openlibrary:"+strings.TrimRight(config.BaseURL, "/"), config.RequestsPerSecond)}
}
func (c *Client) Capability() providers.Capability {
	return providers.Capability{Provider: "openlibrary", EntityKind: "book_source", RawRetention: providers.RetentionPolicy{Class: "provider_raw_48h", Duration: 48 * time.Hour, ObjectPrefix: "ephemeral/48h"}, ResponseCache: providers.ResponseCachePolicy{ReuseDuration: 24 * time.Hour, NegativeDuration: time.Hour, RedisBodyDuration: time.Hour, MaxRedisBodyBytes: 8 << 20}, AcceptedIdentifiers: []providers.Identifier{{Provider: "openlibrary", Namespace: "work"}, {Provider: "openlibrary", Namespace: "edition"}, {Provider: "openlibrary", Namespace: "author"}}, Provides: []providers.Scope{providers.ScopeIdentity, providers.ScopeTitles, providers.ScopeDescriptions, providers.ScopeClassification, providers.ScopeReleases, providers.ScopeRatings, providers.ScopeCredits, providers.ScopeArtwork}}
}
func (c *Client) Search(ctx context.Context, query string, limit int) (providers.Payload, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return providers.Payload{}, fmt.Errorf("Open Library search query is required")
	}
	return c.search(ctx, url.Values{"q": {query}}, url.QueryEscape(query), limit)
}

// SearchByTitleAuthor uses Open Library's structured fields so a common title
// cannot crowd the requested author's work out of the bounded q= result set.
// It deliberately does not constrain publication year: audiobook folder years
// frequently identify an edition rather than the work's first publication.
func (c *Client) SearchByTitleAuthor(ctx context.Context, title, author string, limit int) (providers.Payload, error) {
	title = strings.TrimSpace(title)
	author = strings.TrimSpace(author)
	if title == "" || author == "" {
		return providers.Payload{}, fmt.Errorf("Open Library structured search requires title and author")
	}
	values := url.Values{"title": {title}, "author": {author}}
	return c.search(ctx, values, values.Encode(), limit)
}

func (c *Client) search(ctx context.Context, values url.Values, recordID string, limit int) (providers.Payload, error) {
	if limit < 1 || limit > 100 {
		limit = 20
	}
	values.Set("limit", strconv.Itoa(limit))
	values.Set("fields", "key,title,subtitle,author_key,author_name,first_publish_year,edition_key,edition_count,isbn,language,subject,cover_i,ratings_average,ratings_count")
	return c.get(ctx, "/search.json", values, providers.Payload{Provider: "openlibrary", ProviderNamespace: "work_search", ProviderRecordID: recordID}, 6*time.Hour)
}
func (c *Client) LookupISBN(ctx context.Context, isbn string) (providers.Payload, error) {
	isbn = strings.ToUpper(strings.NewReplacer("-", "", " ", "").Replace(strings.TrimSpace(isbn)))
	if len(isbn) != 10 && len(isbn) != 13 {
		return providers.Payload{}, fmt.Errorf("Open Library ISBN lookup requires ISBN-10 or ISBN-13")
	}
	return c.get(ctx, "/isbn/"+url.PathEscape(isbn)+".json", nil, providers.Payload{Provider: "openlibrary", ProviderNamespace: "isbn_lookup", ProviderRecordID: isbn}, 12*time.Hour)
}
func (c *Client) Collect(ctx context.Context, id providers.Identifier) ([]providers.Payload, error) {
	value, valid := CanonicalKey(id.Value)
	if id.Provider != "openlibrary" || !valid {
		return nil, fmt.Errorf("Open Library collector requires a valid Open Library key")
	}
	suffix := map[string]string{"work": "W", "edition": "M", "author": "A"}[id.Namespace]
	if suffix == "" || !strings.HasSuffix(value, suffix) {
		return nil, fmt.Errorf("Open Library key does not match namespace %q", id.Namespace)
	}
	path := "/" + id.Namespace + "s/" + value + ".json"
	p, err := c.get(ctx, path, nil, providers.Payload{Provider: "openlibrary", ProviderNamespace: id.Namespace, ProviderRecordID: value}, 24*time.Hour)
	if err != nil {
		return nil, err
	}
	return []providers.Payload{p}, nil
}
func (c *Client) Editions(ctx context.Context, work string, limit int) (providers.Payload, error) {
	var valid bool
	work, valid = CanonicalKey(work)
	if !valid || !strings.HasSuffix(work, "W") {
		return providers.Payload{}, fmt.Errorf("invalid Open Library work key")
	}
	if limit < 1 || limit > 100 {
		limit = 50
	}
	return c.get(ctx, "/works/"+work+"/editions.json", url.Values{"limit": {strconv.Itoa(limit)}}, providers.Payload{Provider: "openlibrary", ProviderNamespace: "work_editions", ProviderRecordID: work}, 12*time.Hour)
}
func (c *Client) get(ctx context.Context, path string, values url.Values, payload providers.Payload, reuse time.Duration) (providers.Payload, error) {
	u, err := url.Parse(strings.TrimRight(c.config.BaseURL, "/") + path)
	if err != nil {
		return providers.Payload{}, err
	}
	if values != nil {
		u.RawQuery = values.Encode()
	}
	payload.RequestKey = strings.TrimPrefix(path, "/")
	if u.RawQuery != "" {
		payload.RequestKey += "?" + u.RawQuery
	}
	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return providers.Payload{}, err
	}
	req.Header.Set("Accept", "application/json")
	return c.http.DoPrepared(ctx, req, payload, func(r *http.Request) error {
		if strings.TrimSpace(c.config.UserAgent) == "" {
			return fmt.Errorf("Open Library requires an identified User-Agent")
		}
		if err := c.gate.Wait(ctx); err != nil {
			return err
		}
		r.Header.Set("User-Agent", c.config.UserAgent)
		return nil
	}, func(p *providers.Payload) {
		if p.StatusCode == http.StatusOK {
			var v any
			if json.Unmarshal(p.Body, &v) != nil {
				zero := time.Duration(0)
				p.ReuseDurationOverride = &zero
			} else {
				p.ReuseDurationOverride = &reuse
			}
		}
	})
}

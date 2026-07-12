package acoustid

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
	config config.AcoustIDConfig
	http   *http.Client
	gate   *providers.RequestGate
}
type Response struct {
	Status string `json:"status"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
	Results []Result `json:"results"`
}
type Result struct {
	ID         string      `json:"id"`
	Score      float64     `json:"score"`
	Recordings []Recording `json:"recordings"`
}
type Recording struct {
	ID       string  `json:"id"`
	Title    string  `json:"title"`
	Duration float64 `json:"duration"`
	Artists  []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"artists"`
	ReleaseGroups []struct {
		ID    string `json:"id"`
		Title string `json:"title"`
		Type  string `json:"type"`
	} `json:"releasegroups"`
}

func New(config config.AcoustIDConfig) *Client {
	return &Client{config: config, http: &http.Client{Timeout: 20 * time.Second}, gate: providers.SharedRequestGate("acoustid:"+strings.TrimRight(config.BaseURL, "/"), config.RequestsPerSecond)}
}
func (c *Client) Lookup(ctx context.Context, fingerprint string, duration int, apiKey string) (Response, error) {
	fingerprint = strings.TrimSpace(fingerprint)
	if fingerprint == "" || duration < 1 {
		return Response{}, fmt.Errorf("AcoustID requires a compressed fingerprint and positive duration")
	}
	if apiKey == "" {
		apiKey = c.config.APIKey
	}
	if apiKey == "" {
		return Response{}, fmt.Errorf("AcoustID requires X-Heya-AcoustID-API-Key or HEYA_METADATA_ACOUSTID_API_KEY")
	}
	u, err := url.Parse(strings.TrimRight(c.config.BaseURL, "/") + "/v2/lookup")
	if err != nil {
		return Response{}, err
	}
	q := u.Query()
	q.Set("client", apiKey)
	q.Set("meta", "recordings+releasegroups")
	q.Set("duration", strconv.Itoa(duration))
	q.Set("fingerprint", fingerprint)
	u.RawQuery = q.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return Response{}, err
	}
	request.Header.Set("Accept", "application/json")
	if err = c.gate.Wait(ctx); err != nil {
		return Response{}, err
	}
	response, err := c.http.Do(request)
	if err != nil {
		return Response{}, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Response{}, &providers.StatusError{Provider: "acoustid", StatusCode: response.StatusCode}
	}
	var out Response
	if err = json.NewDecoder(response.Body).Decode(&out); err != nil {
		return Response{}, err
	}
	if out.Status != "ok" {
		if out.Error != nil {
			return Response{}, fmt.Errorf("AcoustID error %d: %s", out.Error.Code, out.Error.Message)
		}
		return Response{}, fmt.Errorf("AcoustID returned status %q", out.Status)
	}
	return out, nil
}

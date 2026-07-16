package tidal

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// tokens caches client-credentials access tokens across client instances.
// Collectors are rebuilt per job, so an instance-scoped cache would perform a
// token exchange for every enrichment run.
var tokens = struct {
	sync.Mutex
	byKey map[string]cachedToken
}{byKey: map[string]cachedToken{}}

type cachedToken struct {
	value     string
	expiresAt time.Time
}

func accessToken(ctx context.Context, authURL, clientID, clientSecret string) (string, error) {
	key := authURL + "\x00" + clientID
	tokens.Lock()
	defer tokens.Unlock()
	if cached, ok := tokens.byKey[key]; ok && time.Now().Before(cached.expiresAt) {
		return cached.value, nil
	}
	form := url.Values{"grant_type": {"client_credentials"}}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, authURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("build Tidal token request: %w", err)
	}
	request.SetBasicAuth(clientID, clientSecret)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := (&http.Client{Timeout: 15 * time.Second}).Do(request)
	if err != nil {
		return "", fmt.Errorf("send Tidal token request: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1024*1024))
	if err != nil {
		return "", fmt.Errorf("read Tidal token response: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Tidal token exchange failed with status %d", response.StatusCode)
	}
	var result struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &result); err != nil || result.AccessToken == "" {
		return "", fmt.Errorf("Tidal token exchange returned an unusable response")
	}
	lifetime := time.Duration(result.ExpiresIn) * time.Second
	if lifetime > 2*time.Minute {
		lifetime -= time.Minute
	}
	tokens.byKey[key] = cachedToken{value: result.AccessToken, expiresAt: time.Now().Add(lifetime)}
	return result.AccessToken, nil
}

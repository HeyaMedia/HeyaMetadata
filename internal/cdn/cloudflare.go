// Package cdn coordinates the shared HTTP edge with deploys. The origin's
// cache contract lets Cloudflare hold API documents and the SPA shell for up
// to five minutes (s-maxage), so a new release must flush the zone once or
// stale shells could reference assets the new build no longer serves.
package cdn

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/redis/go-redis/v9"
)

const purgedVersionKey = "heya:metadata:v2:cloudflare-purged-version"

// versionStore is the slice of go-redis used to remember which build version
// last purged the edge. Racing replicas may both purge; that is harmless.
type versionStore interface {
	Get(ctx context.Context, key string) *redis.StringCmd
	Set(ctx context.Context, key string, value interface{}, expiration time.Duration) *redis.StatusCmd
}

// Purger flushes the Cloudflare zone cache once per deployed build version.
// A zero ZoneID or Token disables it entirely.
type Purger struct {
	ZoneID string
	Token  string
	// APIBaseURL overrides the public Cloudflare API endpoint in tests.
	APIBaseURL string
	Store      versionStore
	Client     *http.Client
}

// PurgeOnDeploy purges the whole zone when version has not purged before.
// "dev" builds never purge so local runs cannot flush the production edge.
// Failures are returned for logging but must not block startup: serving
// slightly stale edge content beats refusing to serve at all.
func (purger Purger) PurgeOnDeploy(ctx context.Context, version string) error {
	if purger.ZoneID == "" || purger.Token == "" {
		return nil
	}
	if version == "" || version == "dev" {
		slog.Debug("cloudflare purge skipped for development build")
		return nil
	}
	last, err := purger.Store.Get(ctx, purgedVersionKey).Result()
	if err == nil && last == version {
		return nil
	}

	if err := purger.purgeEverything(ctx); err != nil {
		return err
	}
	if err := purger.Store.Set(ctx, purgedVersionKey, version, 0).Err(); err != nil {
		return fmt.Errorf("record purged version: %w", err)
	}
	slog.Info("cloudflare cache purged for new deploy", "version", version)
	return nil
}

func (purger Purger) purgeEverything(ctx context.Context) error {
	base := purger.APIBaseURL
	if base == "" {
		base = "https://api.cloudflare.com/client/v4"
	}
	client := purger.Client
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}

	endpoint := fmt.Sprintf("%s/zones/%s/purge_cache", base, purger.ZoneID)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader([]byte(`{"purge_everything":true}`)))
	if err != nil {
		return fmt.Errorf("build purge request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+purger.Token)
	request.Header.Set("Content-Type", "application/json")

	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("purge cloudflare zone: %w", err)
	}
	defer func() { _ = response.Body.Close() }()

	var payload struct {
		Success bool `json:"success"`
		Errors  []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return fmt.Errorf("decode purge response (status %d): %w", response.StatusCode, err)
	}
	if !payload.Success {
		message := "unknown error"
		if len(payload.Errors) > 0 {
			message = payload.Errors[0].Message
		}
		return fmt.Errorf("cloudflare rejected purge (status %d): %s", response.StatusCode, message)
	}
	return nil
}

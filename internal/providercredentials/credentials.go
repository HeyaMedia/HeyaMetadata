// Package providercredentials hands request-scoped upstream credentials to
// asynchronous workers without persisting plaintext secrets in River.
package providercredentials

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const Lifetime = 2 * time.Hour

type Credentials struct {
	APIKeys map[string]string `json:"api_keys,omitempty"`
}

func (c Credentials) APIKey(provider string) string {
	return c.APIKeys[strings.ToLower(strings.TrimSpace(provider))]
}

func (c Credentials) Empty() bool { return len(c.APIKeys) == 0 }

func Store(ctx context.Context, client *redis.Client, credentials Credentials) (string, error) {
	if credentials.Empty() {
		return "", nil
	}
	normalized := Credentials{APIKeys: make(map[string]string, len(credentials.APIKeys))}
	for provider, value := range credentials.APIKeys {
		provider = strings.ToLower(strings.TrimSpace(provider))
		value = strings.TrimSpace(value)
		if strings.TrimSpace(provider) == "" || strings.TrimSpace(value) == "" || len(value) > 1024 {
			return "", fmt.Errorf("invalid provider API key")
		}
		normalized.APIKeys[provider] = value
	}
	token := make([]byte, 32)
	if _, err := rand.Read(token); err != nil {
		return "", fmt.Errorf("generate provider credential reference: %w", err)
	}
	reference := hex.EncodeToString(token)
	body, err := json.Marshal(normalized)
	if err != nil {
		return "", fmt.Errorf("encode provider credentials: %w", err)
	}
	if err := client.Set(ctx, redisKey(reference), body, Lifetime).Err(); err != nil {
		return "", fmt.Errorf("store provider credentials: %w", err)
	}
	return reference, nil
}

func Load(ctx context.Context, client *redis.Client, reference string) (Credentials, error) {
	if reference == "" {
		return Credentials{}, nil
	}
	body, err := client.Get(ctx, redisKey(reference)).Bytes()
	if err != nil {
		if err == redis.Nil {
			return Credentials{}, fmt.Errorf("provider credentials expired before the job ran")
		}
		return Credentials{}, fmt.Errorf("load provider credentials: %w", err)
	}
	var credentials Credentials
	if err := json.Unmarshal(body, &credentials); err != nil {
		return Credentials{}, fmt.Errorf("decode provider credentials: %w", err)
	}
	return credentials, nil
}

func Delete(ctx context.Context, client *redis.Client, reference string) error {
	if reference == "" {
		return nil
	}
	if err := client.Del(ctx, redisKey(reference)).Err(); err != nil {
		return fmt.Errorf("delete provider credentials: %w", err)
	}
	return nil
}

func redisKey(reference string) string {
	return "heya:metadata:v1:provider-credentials:" + reference
}

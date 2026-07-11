// Package providercache reuses exact provider responses across ingestion jobs.
package providercache

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/blobstore"
	"github.com/HeyaMedia/HeyaMetadata/internal/ingest"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers"
	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"
)

var ErrFetchInProgress = errors.New("an identical provider request is already being fetched")

type Resolver struct {
	runtime           *platform.Runtime
	normalizerVersion string
	retention         providers.RetentionPolicy
	policy            providers.ResponseCachePolicy
	riverJobID        int64
}

type pointer struct {
	ObservationID string      `json:"observation_id"`
	Checksum      string      `json:"checksum"`
	ObjectKey     string      `json:"object_key"`
	StatusCode    int         `json:"status_code"`
	Headers       http.Header `json:"headers"`
	ObservedAt    time.Time   `json:"observed_at"`
	ReusableUntil time.Time   `json:"reusable_until"`
}

func New(runtime *platform.Runtime, normalizerVersion string, retention providers.RetentionPolicy, policy providers.ResponseCachePolicy, riverJobID int64) (*Resolver, error) {
	if runtime == nil || runtime.DB == nil || runtime.Redis == nil || runtime.Blobs == nil {
		return nil, fmt.Errorf("provider cache runtime is incomplete")
	}
	if retention.Class == "" || retention.Duration <= 0 || retention.ObjectPrefix == "" {
		return nil, fmt.Errorf("provider cache retention policy is incomplete")
	}
	if policy.ReuseDuration <= 0 || policy.ReuseDuration > retention.Duration {
		return nil, fmt.Errorf("provider reuse duration must be positive and no longer than blob retention")
	}
	if policy.NegativeDuration < 0 || policy.NegativeDuration > retention.Duration || policy.RedisBodyDuration < 0 || policy.MaxRedisBodyBytes < 0 {
		return nil, fmt.Errorf("provider Redis body policy is invalid")
	}
	return &Resolver{runtime: runtime, normalizerVersion: normalizerVersion, retention: retention, policy: policy, riverJobID: riverJobID}, nil
}

func (r *Resolver) Resolve(ctx context.Context, template providers.Payload, fetch func() (providers.Payload, error)) (providers.Payload, error) {
	fingerprint := providers.RequestFingerprint(template.Provider, template.RequestKey)
	if payload, ok, err := r.lookup(ctx, template, fingerprint); err != nil {
		return providers.Payload{}, err
	} else if ok {
		return payload, nil
	}

	lockKey := "heya:metadata:v1:lock:provider-fetch:" + template.Provider + ":" + fingerprint
	token, err := randomToken()
	if err != nil {
		return providers.Payload{}, err
	}
	acquired, err := r.runtime.Redis.SetNX(ctx, lockKey, token, 2*time.Minute).Result()
	if err != nil {
		return providers.Payload{}, fmt.Errorf("acquire provider fetch lock: %w", err)
	}
	if !acquired {
		deadline := time.NewTimer(10 * time.Second)
		defer deadline.Stop()
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return providers.Payload{}, ctx.Err()
			case <-deadline.C:
				return providers.Payload{}, ErrFetchInProgress
			case <-ticker.C:
				if payload, ok, lookupErr := r.lookup(ctx, template, fingerprint); lookupErr != nil {
					return providers.Payload{}, lookupErr
				} else if ok {
					return payload, nil
				}
			}
		}
	}
	defer r.unlock(context.WithoutCancel(ctx), lockKey, token)

	// Another worker may have populated the cache between our first lookup and lock.
	if payload, ok, err := r.lookup(ctx, template, fingerprint); err != nil {
		return providers.Payload{}, err
	} else if ok {
		return payload, nil
	}

	payload, err := fetch()
	if err != nil {
		return providers.Payload{}, err
	}
	recorded, err := ingest.RecordObservation(ctx, r.runtime, payload, r.normalizerVersion, r.retention, r.policy, r.riverJobID)
	if err != nil {
		return providers.Payload{}, err
	}
	payload.ObservationID = recorded.ID
	payload.BlobChecksum = recorded.Checksum
	if duration := r.policy.DurationForPayload(payload); duration > 0 {
		p := pointer{ObservationID: recorded.ID, Checksum: recorded.Checksum, ObjectKey: recorded.ObjectKey, StatusCode: payload.StatusCode, Headers: safeHeaders(payload.Headers), ObservedAt: payload.ObservedAt, ReusableUntil: payload.ObservedAt.Add(duration)}
		if err := r.remember(ctx, template.Provider, fingerprint, p, payload.Body); err != nil {
			return providers.Payload{}, err
		}
	}
	return payload, nil
}

func (r *Resolver) lookup(ctx context.Context, template providers.Payload, fingerprint string) (providers.Payload, bool, error) {
	redisKey := pointerKey(template.Provider, fingerprint)
	if raw, err := r.runtime.Redis.Get(ctx, redisKey).Bytes(); err == nil {
		var p pointer
		if json.Unmarshal(raw, &p) == nil && time.Now().Before(p.ReusableUntil) {
			return r.materialize(ctx, template, fingerprint, p)
		}
		_ = r.runtime.Redis.Del(ctx, redisKey).Err()
	} else if err != redis.Nil {
		return providers.Payload{}, false, fmt.Errorf("read provider cache pointer: %w", err)
	}

	var p pointer
	var headersJSON []byte
	err := r.runtime.DB.QueryRow(ctx, `
		SELECT po.id, po.blob_checksum, sb.object_key, po.response_status,
		       po.response_headers, po.observed_at, po.reusable_until
		FROM provider_observations po
		JOIN source_blobs sb ON sb.checksum = po.blob_checksum
		WHERE po.provider = $1 AND po.request_fingerprint = $2
		  AND (po.response_status BETWEEN 200 AND 299 OR po.response_status = 404)
		  AND po.reusable_until > now()
		  AND sb.deleted_at IS NULL
		  AND (sb.expires_at IS NULL OR sb.expires_at > now())
		ORDER BY po.observed_at DESC
		LIMIT 1`, template.Provider, fingerprint).Scan(
		&p.ObservationID, &p.Checksum, &p.ObjectKey, &p.StatusCode,
		&headersJSON, &p.ObservedAt, &p.ReusableUntil,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return providers.Payload{}, false, nil
		}
		return providers.Payload{}, false, fmt.Errorf("read durable provider cache pointer: %w", err)
	}
	_ = json.Unmarshal(headersJSON, &p.Headers)
	return r.materialize(ctx, template, fingerprint, p)
}

func (r *Resolver) materialize(ctx context.Context, template providers.Payload, fingerprint string, p pointer) (providers.Payload, bool, error) {
	bodyKey := "heya:metadata:v1:provider-body:" + p.Checksum
	body, err := r.runtime.Redis.Get(ctx, bodyKey).Bytes()
	hotBody := err == nil
	if err != nil && err != redis.Nil {
		return providers.Payload{}, false, fmt.Errorf("read provider hot body: %w", err)
	}
	if err == redis.Nil {
		stored, getErr := r.runtime.Blobs.Get(ctx, p.ObjectKey)
		if getErr != nil {
			if errors.Is(getErr, blobstore.ErrNotFound) {
				// A lifecycle migration may have moved this checksum after the Redis
				// pointer was written. Retry the current durable object key before
				// declaring the blob missing.
				var currentObjectKey string
				queryErr := r.runtime.DB.QueryRow(ctx, `
					SELECT object_key FROM source_blobs
					WHERE checksum = $1 AND deleted_at IS NULL
					  AND (expires_at IS NULL OR expires_at > now())`, p.Checksum).Scan(&currentObjectKey)
				if queryErr == nil && currentObjectKey != p.ObjectKey {
					p.ObjectKey = currentObjectKey
					return r.materialize(ctx, template, fingerprint, p)
				}
				if queryErr != nil && !errors.Is(queryErr, pgx.ErrNoRows) {
					return providers.Payload{}, false, fmt.Errorf("refresh cached provider object key: %w", queryErr)
				}
				_ = r.runtime.Redis.Del(ctx, pointerKey(template.Provider, fingerprint)).Err()
				_, _ = r.runtime.DB.Exec(ctx, `UPDATE source_blobs SET deleted_at = now() WHERE checksum = $1`, p.Checksum)
				return providers.Payload{}, false, nil
			}
			return providers.Payload{}, false, fmt.Errorf("load cached provider body: %w", getErr)
		}
		body, err = decompress(stored)
		if err != nil {
			return providers.Payload{}, false, fmt.Errorf("decompress cached provider body: %w", err)
		}
	}
	digest := sha256.Sum256(body)
	if hex.EncodeToString(digest[:]) != p.Checksum {
		if hotBody {
			_ = r.runtime.Redis.Del(ctx, bodyKey).Err()
			return r.materialize(ctx, template, fingerprint, p)
		}
		return providers.Payload{}, false, fmt.Errorf("cached provider body checksum mismatch")
	}
	if err := r.remember(ctx, template.Provider, fingerprint, p, body); err != nil {
		return providers.Payload{}, false, err
	}
	template.StatusCode = p.StatusCode
	template.Headers = p.Headers.Clone()
	template.Body = body
	template.ObservedAt = p.ObservedAt
	template.ObservationID = p.ObservationID
	template.BlobChecksum = p.Checksum
	template.FromCache = true
	return template, true, nil
}

func (r *Resolver) remember(ctx context.Context, provider, fingerprint string, p pointer, body []byte) error {
	ttl := time.Until(p.ReusableUntil)
	if ttl <= 0 {
		return nil
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("encode provider cache pointer: %w", err)
	}
	pipe := r.runtime.Redis.TxPipeline()
	pipe.Set(ctx, pointerKey(provider, fingerprint), raw, ttl)
	if r.policy.RedisBodyDuration > 0 && len(body) <= r.policy.MaxRedisBodyBytes {
		bodyTTL := min(r.policy.RedisBodyDuration, ttl)
		pipe.Set(ctx, "heya:metadata:v1:provider-body:"+p.Checksum, body, bodyTTL)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("write provider cache: %w", err)
	}
	return nil
}

func pointerKey(provider, fingerprint string) string {
	return "heya:metadata:v1:provider-cache:" + provider + ":" + fingerprint
}

func safeHeaders(headers http.Header) http.Header {
	selected := make(http.Header)
	for _, name := range []string{"Cache-Control", "Content-Type", "ETag", "Last-Modified", "X-RateLimit-Limit", "X-RateLimit-Remaining", "X-RateLimit-Reset"} {
		if value := headers.Get(name); value != "" {
			selected.Set(name, value)
		}
	}
	return selected
}

func decompress(body []byte) ([]byte, error) {
	if len(body) < 2 || body[0] != 0x1f || body[1] != 0x8b {
		return body, nil
	}
	reader, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	return io.ReadAll(io.LimitReader(reader, 32*1024*1024))
}

func randomToken() (string, error) {
	value := make([]byte, 24)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate provider lock token: %w", err)
	}
	return hex.EncodeToString(value), nil
}

func (r *Resolver) unlock(ctx context.Context, key, token string) {
	const compareAndDelete = `if redis.call("get", KEYS[1]) == ARGV[1] then return redis.call("del", KEYS[1]) else return 0 end`
	_ = r.runtime.Redis.Eval(ctx, compareAndDelete, []string{key}, token).Err()
}

// Package ingest persists provider responses without knowing their domain.
package ingest

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers"
)

type RecordedObservation struct {
	ID        string
	Checksum  string
	ObjectKey string
	Payload   providers.Payload
}

func RecordObservation(
	ctx context.Context,
	runtime *platform.Runtime,
	payload providers.Payload,
	normalizerVersion string,
	retention providers.RetentionPolicy,
	cachePolicy providers.ResponseCachePolicy,
	riverJobID int64,
) (RecordedObservation, error) {
	if retention.Class == "" || retention.Duration <= 0 || retention.ObjectPrefix == "" {
		return RecordedObservation{}, fmt.Errorf("provider observation retention policy is incomplete")
	}
	digest := sha256.Sum256(payload.Body)
	checksum := hex.EncodeToString(digest[:])
	var previousObjectKey string
	var previousExpiresAt *time.Time
	_ = runtime.DB.QueryRow(ctx, `SELECT object_key, expires_at FROM source_blobs WHERE checksum = $1`, checksum).Scan(&previousObjectKey, &previousExpiresAt)
	objectKey, err := runtime.Blobs.ContentKeyUnder(retention.ObjectPrefix, checksum, ".json.gz")
	if err != nil {
		return RecordedObservation{}, err
	}
	if previousObjectKey != "" && previousExpiresAt == nil {
		objectKey = previousObjectKey
	}

	var compressed bytes.Buffer
	compressor, err := gzip.NewWriterLevel(&compressed, gzip.BestCompression)
	if err != nil {
		return RecordedObservation{}, fmt.Errorf("create observation compressor: %w", err)
	}
	if _, err := compressor.Write(payload.Body); err != nil {
		return RecordedObservation{}, fmt.Errorf("compress observation: %w", err)
	}
	if err := compressor.Close(); err != nil {
		return RecordedObservation{}, fmt.Errorf("finish observation compression: %w", err)
	}
	mediaType := payload.Headers.Get("Content-Type")
	if mediaType == "" {
		mediaType = "application/json"
	}
	if err := runtime.Blobs.PutImmutable(ctx, objectKey, compressed.Bytes(), mediaType, "gzip"); err != nil {
		return RecordedObservation{}, fmt.Errorf("store provider observation: %w", err)
	}

	selectedHeaders, err := json.Marshal(selectHeaders(payload.Headers))
	if err != nil {
		return RecordedObservation{}, fmt.Errorf("encode observation headers: %w", err)
	}
	requestFingerprint := providers.RequestFingerprint(payload.Provider, payload.RequestKey)

	tx, err := runtime.DB.Begin(ctx)
	if err != nil {
		return RecordedObservation{}, fmt.Errorf("begin observation transaction: %w", err)
	}
	defer tx.Rollback(ctx)
	expiresAt := payload.ObservedAt.Add(retention.Duration)
	var reusableUntil *time.Time
	if duration := cachePolicy.DurationForPayload(payload); duration > 0 {
		value := payload.ObservedAt.Add(duration)
		reusableUntil = &value
	}
	if _, err := tx.Exec(ctx, `
        INSERT INTO source_blobs (
            checksum, object_key, compression, media_type, uncompressed_size,
            compressed_size, integrity_state, retention_class, expires_at
        ) VALUES ($1, $2, 'gzip', $3, $4, $5, 'verified', $6, $7)
        ON CONFLICT (checksum) DO UPDATE SET
            object_key = EXCLUDED.object_key,
            retention_class = CASE
                WHEN source_blobs.expires_at IS NULL THEN source_blobs.retention_class
                ELSE EXCLUDED.retention_class
            END,
            expires_at = CASE
                WHEN source_blobs.expires_at IS NULL OR EXCLUDED.expires_at IS NULL THEN NULL
                ELSE LEAST(source_blobs.expires_at, EXCLUDED.expires_at)
            END,
            deleted_at = NULL`,
		checksum, objectKey, mediaType, len(payload.Body), compressed.Len(), retention.Class, expiresAt,
	); err != nil {
		return RecordedObservation{}, fmt.Errorf("record source blob: %w", err)
	}
	var observationID string
	if err := tx.QueryRow(ctx, `
        INSERT INTO provider_observations (
            provider, provider_namespace, provider_record_id, request_key,
            response_status, response_time_ms, observed_at, blob_checksum,
            normalizer_version, retention_class, river_job_id, response_headers,
			request_fingerprint, reusable_until
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
        RETURNING id`,
		payload.Provider, payload.ProviderNamespace, payload.ProviderRecordID, payload.RequestKey,
		payload.StatusCode, payload.ResponseTime.Milliseconds(), payload.ObservedAt, checksum,
		normalizerVersion, retention.Class, nullableJobID(riverJobID), selectedHeaders, requestFingerprint, reusableUntil,
	).Scan(&observationID); err != nil {
		return RecordedObservation{}, fmt.Errorf("record provider observation: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return RecordedObservation{}, fmt.Errorf("commit provider observation: %w", err)
	}
	if previousObjectKey != "" && previousObjectKey != objectKey {
		if err := runtime.Blobs.Delete(ctx, previousObjectKey); err != nil {
			return RecordedObservation{}, fmt.Errorf("delete superseded provider blob %q: %w", previousObjectKey, err)
		}
	}
	return RecordedObservation{ID: observationID, Checksum: checksum, ObjectKey: objectKey, Payload: payload}, nil
}

func selectHeaders(headers http.Header) map[string]string {
	selected := map[string]string{}
	for _, name := range []string{
		"Cache-Control", "Content-Type", "ETag", "Last-Modified",
		"X-RateLimit-Limit", "X-RateLimit-Remaining", "X-RateLimit-Reset",
	} {
		if value := strings.TrimSpace(headers.Get(name)); value != "" {
			selected[http.CanonicalHeaderKey(name)] = value
		}
	}
	return selected
}

func nullableJobID(jobID int64) any {
	if jobID == 0 {
		return nil
	}
	return jobID
}

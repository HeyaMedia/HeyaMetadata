package jobs

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/blobstore"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
)

const PlatformSmokeKind = "platform_smoke_v1"

type PlatformSmokeArgs struct {
	Nonce       string    `json:"nonce"`
	RequestedAt time.Time `json:"requested_at"`
}

func (PlatformSmokeArgs) Kind() string { return PlatformSmokeKind }

type PlatformSmokeWorker struct {
	river.WorkerDefaults[PlatformSmokeArgs]
	runtime *platform.Runtime
}

func NewPlatformSmokeWorker(runtime *platform.Runtime) *PlatformSmokeWorker {
	return &PlatformSmokeWorker{runtime: runtime}
}

func (w *PlatformSmokeWorker) Work(ctx context.Context, job *river.Job[PlatformSmokeArgs]) error {
	raw, err := json.Marshal(struct {
		Kind        string    `json:"kind"`
		Nonce       string    `json:"nonce"`
		RequestedAt time.Time `json:"requested_at"`
	}{
		Kind: PlatformSmokeKind, Nonce: job.Args.Nonce, RequestedAt: job.Args.RequestedAt.UTC(),
	})
	if err != nil {
		return fmt.Errorf("encode smoke observation: %w", err)
	}
	digest := sha256.Sum256(raw)
	checksum := hex.EncodeToString(digest[:])
	objectKey, err := blobstore.ContentKey(checksum, ".json.gz")
	if err != nil {
		return err
	}

	var compressed bytes.Buffer
	compressor, err := gzip.NewWriterLevel(&compressed, gzip.BestCompression)
	if err != nil {
		return fmt.Errorf("create gzip writer: %w", err)
	}
	if _, err := compressor.Write(raw); err != nil {
		return fmt.Errorf("compress smoke observation: %w", err)
	}
	if err := compressor.Close(); err != nil {
		return fmt.Errorf("finish smoke observation compression: %w", err)
	}
	if err := w.runtime.Blobs.PutImmutable(ctx, objectKey, compressed.Bytes(), "application/json", "gzip"); err != nil {
		return err
	}
	stored, err := w.runtime.Blobs.Get(ctx, objectKey)
	if err != nil {
		return err
	}
	if !bytes.Equal(stored, compressed.Bytes()) {
		return fmt.Errorf("S3 round trip changed smoke blob bytes")
	}

	redisKey := "heya:metadata:v1:platform-smoke:" + job.Args.Nonce
	if err := w.runtime.Redis.Set(ctx, redisKey, checksum, time.Minute).Err(); err != nil {
		return fmt.Errorf("write Redis smoke value: %w", err)
	}
	redisValue, err := w.runtime.Redis.Get(ctx, redisKey).Result()
	if err != nil {
		return fmt.Errorf("read Redis smoke value: %w", err)
	}
	_ = w.runtime.Redis.Del(ctx, redisKey).Err()
	if redisValue != checksum {
		return fmt.Errorf("Redis round trip changed smoke value")
	}

	tx, err := w.runtime.DB.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin smoke transaction: %w", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `
        INSERT INTO source_blobs (
            checksum, object_key, compression, media_type,
            uncompressed_size, compressed_size, integrity_state, retention_class
        ) VALUES ($1, $2, 'gzip', 'application/json', $3, $4, 'verified', 'platform_smoke')
        ON CONFLICT (checksum) DO NOTHING`,
		checksum, objectKey, len(raw), compressed.Len(),
	); err != nil {
		return fmt.Errorf("record smoke source blob: %w", err)
	}

	var observationID string
	err = tx.QueryRow(ctx, `
        INSERT INTO provider_observations (
            provider, provider_namespace, provider_record_id, request_key,
            response_status, observed_at, blob_checksum, normalizer_version,
            retention_class, river_job_id
        ) VALUES ('platform', 'smoke', $1, $2, 200, $3, $4, 'platform-smoke/v1', 'platform_smoke', $5)
        ON CONFLICT (provider, provider_namespace, provider_record_id, request_key, observed_at)
        DO NOTHING
        RETURNING id`,
		job.Args.Nonce, fmt.Sprintf("river:%d", job.ID), job.Args.RequestedAt.UTC(), checksum, job.ID,
	).Scan(&observationID)
	if err == pgx.ErrNoRows {
		err = tx.QueryRow(ctx, `
            SELECT id
            FROM provider_observations
            WHERE provider = 'platform'
              AND provider_namespace = 'smoke'
              AND provider_record_id = $1
              AND request_key = $2
              AND observed_at = $3`,
			job.Args.Nonce, fmt.Sprintf("river:%d", job.ID), job.Args.RequestedAt.UTC(),
		).Scan(&observationID)
	}
	if err != nil {
		return fmt.Errorf("record smoke observation: %w", err)
	}
	if _, err := tx.Exec(ctx, `
        INSERT INTO platform_smoke_runs (river_job_id, observation_id, blob_checksum, redis_roundtrip)
        VALUES ($1, $2, $3, true)
        ON CONFLICT (river_job_id) DO UPDATE SET
            observation_id = EXCLUDED.observation_id,
            blob_checksum = EXCLUDED.blob_checksum,
            redis_roundtrip = EXCLUDED.redis_roundtrip,
            completed_at = now()`,
		job.ID, observationID, checksum,
	); err != nil {
		return fmt.Errorf("record smoke completion: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit smoke transaction: %w", err)
	}

	slog.Info("platform smoke job completed", "job_id", job.ID, "blob_checksum", checksum)
	return nil
}

package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/riverqueue/river"
)

const BlobRetentionKind = "blob_retention_v1"

type BlobRetentionArgs struct{}

func (BlobRetentionArgs) Kind() string { return BlobRetentionKind }

func (BlobRetentionArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Priority:   PriorityScheduled,
		UniqueOpts: river.UniqueOpts{ByArgs: true, ByState: activeJobStates()},
	}
}

type BlobRetentionWorker struct {
	river.WorkerDefaults[BlobRetentionArgs]
	runtime *platform.Runtime
}

func NewBlobRetentionWorker(runtime *platform.Runtime) *BlobRetentionWorker {
	return &BlobRetentionWorker{runtime: runtime}
}

func (w *BlobRetentionWorker) Work(ctx context.Context, _ *river.Job[BlobRetentionArgs]) error {
	_, err := SweepExpiredBlobs(ctx, w.runtime, 500, 24*time.Hour)
	return err
}

func SweepExpiredBlobs(ctx context.Context, runtime *platform.Runtime, limit int, lifecycleGrace time.Duration) (int, error) {
	if limit < 1 || limit > 5000 {
		limit = 500
	}
	if lifecycleGrace < 0 {
		lifecycleGrace = 0
	}
	rows, err := runtime.DB.Query(ctx, `
        SELECT checksum, object_key
        FROM source_blobs
        WHERE expires_at <= now() - ($2 * interval '1 second') AND deleted_at IS NULL
        ORDER BY expires_at
		LIMIT $1`, limit, lifecycleGrace.Seconds())
	if err != nil {
		return 0, fmt.Errorf("select expired source blobs: %w", err)
	}
	type expiredBlob struct{ checksum, objectKey string }
	var expired []expiredBlob
	for rows.Next() {
		var blob expiredBlob
		if err := rows.Scan(&blob.checksum, &blob.objectKey); err != nil {
			rows.Close()
			return 0, err
		}
		expired = append(expired, blob)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()
	for _, blob := range expired {
		if err := runtime.Blobs.Delete(ctx, blob.objectKey); err != nil {
			return 0, err
		}
		if _, err := runtime.DB.Exec(ctx, `
            UPDATE source_blobs SET deleted_at = now()
			WHERE checksum = $1 AND expires_at <= now() - ($2 * interval '1 second') AND deleted_at IS NULL`, blob.checksum, lifecycleGrace.Seconds()); err != nil {
			return 0, fmt.Errorf("mark source blob expired: %w", err)
		}
	}
	if len(expired) > 0 {
		slog.Info("expired provider source blobs", "count", len(expired))
	}
	return len(expired), nil
}

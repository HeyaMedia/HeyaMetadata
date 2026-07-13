package images

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"
)

const (
	ColdAfter      = 180 * 24 * time.Hour
	OrphanGrace    = 24 * time.Hour
	accessKey      = "heya:metadata:v2:images:last-access"
	accessBatchMax = 5000
)

func registerObject(ctx context.Context, runtime *platform.Runtime, key, mediaType string, byteSize int64) error {
	_, err := runtime.DB.Exec(ctx, `
		INSERT INTO image_cache_objects(object_key,media_type,byte_size)
		VALUES($1,$2,$3)
		ON CONFLICT(object_key) DO UPDATE SET last_seen_at=now()`, key, mediaType, byteSize)
	if err != nil {
		return fmt.Errorf("register image cache object: %w", err)
	}
	return nil
}

func trackAccess(ctx context.Context, runtime *platform.Runtime, imageID string) {
	if runtime.Redis == nil {
		return
	}
	// A sorted set coalesces arbitrarily many reads of one image into one
	// timestamp, keeping hot-path writes out of Postgres.
	_ = runtime.Redis.ZAdd(ctx, accessKey, redis.Z{Score: float64(time.Now().UTC().Unix()), Member: imageID}).Err()
}

func FlushAccesses(ctx context.Context, runtime *platform.Runtime, limit int64) (int, error) {
	if limit < 1 || limit > accessBatchMax {
		limit = accessBatchMax
	}
	claimed, err := runtime.Redis.ZPopMin(ctx, accessKey, limit).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return 0, fmt.Errorf("claim image accesses: %w", err)
	}
	if len(claimed) == 0 {
		return 0, nil
	}
	restore := func() {
		entries := make([]redis.Z, 0, len(claimed))
		for _, entry := range claimed {
			entries = append(entries, redis.Z{Score: entry.Score, Member: entry.Member})
		}
		_ = runtime.Redis.ZAddArgs(context.WithoutCancel(ctx), accessKey, redis.ZAddArgs{GT: true, Members: entries}).Err()
	}
	tx, err := runtime.DB.Begin(ctx)
	if err != nil {
		restore()
		return 0, err
	}
	defer tx.Rollback(ctx)
	for _, entry := range claimed {
		imageID, ok := entry.Member.(string)
		if !ok || imageID == "" {
			continue
		}
		accessedAt := time.Unix(int64(entry.Score), 0).UTC()
		if _, err := tx.Exec(ctx, `UPDATE image_candidates SET last_accessed_at=GREATEST(COALESCE(last_accessed_at,'-infinity'::timestamptz),$2) WHERE id=$1`, imageID, accessedAt); err != nil {
			restore()
			return 0, fmt.Errorf("persist image access: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		restore()
		return 0, fmt.Errorf("commit image accesses: %w", err)
	}
	return len(claimed), nil
}

func SweepCold(ctx context.Context, runtime *platform.Runtime, limit int, coldAfter time.Duration) (int, error) {
	if limit < 1 || limit > 5000 {
		limit = 500
	}
	if coldAfter <= 0 {
		coldAfter = ColdAfter
	}
	cutoff := time.Now().UTC().Add(-coldAfter)
	rows, err := runtime.DB.Query(ctx, `
		SELECT id
		FROM image_candidates
		WHERE materialization_state='ready'
		  AND COALESCE(last_accessed_at,materialized_at,created_at)<$1
		ORDER BY COALESCE(last_accessed_at,materialized_at,created_at)
		LIMIT $2`, cutoff, limit)
	if err != nil {
		return 0, fmt.Errorf("select cold images: %w", err)
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()

	evicted := 0
	for _, id := range ids {
		// Protect an access that arrived after the periodic flush but before this
		// sweep. The conditional SQL update below protects persisted accesses.
		if score, scoreErr := runtime.Redis.ZScore(ctx, accessKey, id).Result(); scoreErr == nil && score >= float64(cutoff.Unix()) {
			continue
		}
		keys, ok, err := evictCandidate(ctx, runtime, id, cutoff)
		if err != nil {
			return evicted, err
		}
		if !ok {
			continue
		}
		for _, key := range keys {
			if err := deleteIfUnreferenced(ctx, runtime, key); err != nil {
				return evicted, err
			}
		}
		evicted++
	}
	if evicted > 0 {
		slog.Info("evicted cold image bundles", "count", evicted, "inactive_for", coldAfter)
	}
	return evicted, nil
}

func RecoverStalled(ctx context.Context, runtime *platform.Runtime, stalledAfter time.Duration) (int64, error) {
	if stalledAfter <= 0 {
		stalledAfter = 30 * time.Minute
	}
	result, err := runtime.DB.Exec(ctx, `
		UPDATE image_candidates
		SET materialization_state='failed',materialization_error='materialization worker stopped before completion'
		WHERE materialization_state='working'
		  AND materialization_attempted_at<now()-($1*interval '1 second')`, stalledAfter.Seconds())
	if err != nil {
		return 0, fmt.Errorf("recover stalled image materializations: %w", err)
	}
	return result.RowsAffected(), nil
}

func SweepOrphans(ctx context.Context, runtime *platform.Runtime, limit int, grace time.Duration) (int, error) {
	if limit < 1 || limit > 5000 {
		limit = 500
	}
	if grace <= 0 {
		grace = OrphanGrace
	}
	rows, err := runtime.DB.Query(ctx, `
		SELECT o.object_key
		FROM image_cache_objects o
		WHERE o.created_at<now()-($2*interval '1 second')
		  AND NOT EXISTS(SELECT 1 FROM image_candidates c WHERE c.object_key=o.object_key)
		  AND NOT EXISTS(SELECT 1 FROM image_variants v WHERE v.object_key=o.object_key)
		ORDER BY o.created_at LIMIT $1`, limit, grace.Seconds())
	if err != nil {
		return 0, fmt.Errorf("select orphaned image objects: %w", err)
	}
	var keys []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			rows.Close()
			return 0, err
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()
	for _, key := range keys {
		if err := deleteIfUnreferenced(ctx, runtime, key); err != nil {
			return 0, err
		}
	}
	return len(keys), nil
}

func evictCandidate(ctx context.Context, runtime *platform.Runtime, id string, cutoff time.Time) ([]string, bool, error) {
	tx, err := runtime.DB.Begin(ctx)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback(ctx)
	var originalKey string
	err = tx.QueryRow(ctx, `
		SELECT COALESCE(object_key,'') FROM image_candidates
		WHERE id=$1 AND materialization_state='ready'
		  AND COALESCE(last_accessed_at,materialized_at,created_at)<$2
		FOR UPDATE`, id, cutoff).Scan(&originalKey)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("claim cold image %s: %w", id, err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE image_candidates SET
			materialization_state='pending',blob_checksum=NULL,object_key=NULL,
			media_type=NULL,byte_size=NULL,materialized_at=NULL,
			materialized_width=NULL,materialized_height=NULL,evicted_at=now()
		WHERE id=$1`, id); err != nil {
		return nil, false, fmt.Errorf("reset cold image %s: %w", id, err)
	}
	keys := []string{originalKey}
	rows, err := tx.Query(ctx, `DELETE FROM image_variants WHERE image_id=$1 RETURNING object_key`, id)
	if err != nil {
		return nil, false, fmt.Errorf("delete image variants: %w", err)
	}
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			rows.Close()
			return nil, false, err
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, false, err
	}
	rows.Close()
	if err := tx.Commit(ctx); err != nil {
		return nil, false, err
	}
	return uniqueNonEmpty(keys), true, nil
}

func deleteIfUnreferenced(ctx context.Context, runtime *platform.Runtime, key string) error {
	var referenced bool
	if err := runtime.DB.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM image_candidates WHERE object_key=$1)
		    OR EXISTS(SELECT 1 FROM image_variants WHERE object_key=$1)`, key).Scan(&referenced); err != nil {
		return fmt.Errorf("check image object references: %w", err)
	}
	if referenced {
		return nil
	}
	if err := runtime.Blobs.Delete(ctx, key); err != nil {
		return fmt.Errorf("delete cold image object %q: %w", key, err)
	}
	if _, err := runtime.DB.Exec(ctx, `DELETE FROM image_cache_objects WHERE object_key=$1`, key); err != nil {
		return fmt.Errorf("remove image object registry entry: %w", err)
	}
	return nil
}

func uniqueNonEmpty(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

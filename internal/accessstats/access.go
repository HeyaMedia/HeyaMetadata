// Package accessstats tracks demand and turns it into adaptive refresh cadence.
package accessstats

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/redis/go-redis/v9"
)

const (
	pendingKey    = "heya:metadata:v1:access:pending"
	lastAccessKey = "heya:metadata:v1:access:last"
)

func Track(ctx context.Context, client *redis.Client, entityID string) error {
	pipe := client.TxPipeline()
	pipe.ZIncrBy(ctx, pendingKey, 1, entityID)
	pipe.HSet(ctx, lastAccessKey, entityID, time.Now().UTC().UnixMilli())
	_, err := pipe.Exec(ctx)
	return err
}

// Flush persists at most limit buffered entity counters. ZPOPMAX atomically
// claims each counter; a failed transaction restores the claimed increments.
func Flush(ctx context.Context, runtime *platform.Runtime, limit int64) (int, error) {
	if limit < 1 || limit > 5000 {
		limit = 500
	}
	claimed, err := runtime.Redis.ZPopMax(ctx, pendingKey, limit).Result()
	if err != nil && err != redis.Nil {
		return 0, fmt.Errorf("claim access counters: %w", err)
	}
	if len(claimed) == 0 {
		return 0, nil
	}
	restore := func() {
		pipe := runtime.Redis.TxPipeline()
		for _, entry := range claimed {
			if entityID, ok := entry.Member.(string); ok && entityID != "" {
				pipe.ZIncrBy(context.WithoutCancel(ctx), pendingKey, entry.Score, entityID)
			}
		}
		_, _ = pipe.Exec(context.WithoutCancel(ctx))
	}
	tx, err := runtime.DB.Begin(ctx)
	if err != nil {
		restore()
		return 0, fmt.Errorf("begin access stats flush: %w", err)
	}
	defer tx.Rollback(ctx)
	for _, entry := range claimed {
		entityID, ok := entry.Member.(string)
		if !ok || entityID == "" {
			continue
		}
		lastAccessed := time.Now().UTC()
		if raw, getErr := runtime.Redis.HGet(ctx, lastAccessKey, entityID).Result(); getErr == nil {
			if milliseconds, parseErr := strconv.ParseInt(raw, 10, 64); parseErr == nil {
				lastAccessed = time.UnixMilli(milliseconds).UTC()
			}
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO entity_access_stats (
				entity_id, total_fetches, decayed_score, last_accessed_at, score_updated_at
			) VALUES ($1, $2::bigint, $2::double precision, $3, now())
			ON CONFLICT (entity_id) DO UPDATE SET
				total_fetches = entity_access_stats.total_fetches + EXCLUDED.total_fetches,
				decayed_score = entity_access_stats.decayed_score *
					exp(-EXTRACT(EPOCH FROM (now() - entity_access_stats.score_updated_at)) / 604800.0)
					+ EXCLUDED.decayed_score,
				last_accessed_at = GREATEST(entity_access_stats.last_accessed_at, EXCLUDED.last_accessed_at),
				score_updated_at = now(), updated_at = now()`, entityID, int64(entry.Score), lastAccessed); err != nil {
			restore()
			return 0, fmt.Errorf("upsert entity access stats: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		restore()
		return 0, fmt.Errorf("commit access stats flush: %w", err)
	}
	return len(claimed), nil
}

// RecalculateRefreshCadence maps demand onto a 2/7/14/30-day schedule. Scores
// decay with a seven-day time constant even without new reads.
func RecalculateRefreshCadence(ctx context.Context, runtime *platform.Runtime) error {
	_, err := runtime.DB.Exec(ctx, `
		WITH cadence AS (
			SELECT prs.entity_id, prs.provider,
				CASE
					WHEN stats.last_accessed_at >= now() - interval '2 days'
					  OR COALESCE(stats.decayed_score * exp(-EXTRACT(EPOCH FROM (now() - stats.score_updated_at)) / 604800.0), 0) >= 20
						THEN interval '2 days'
					WHEN stats.last_accessed_at >= now() - interval '14 days'
					  OR COALESCE(stats.decayed_score * exp(-EXTRACT(EPOCH FROM (now() - stats.score_updated_at)) / 604800.0), 0) >= 5
						THEN interval '7 days'
					WHEN stats.last_accessed_at >= now() - interval '60 days'
						THEN interval '14 days'
					ELSE interval '30 days'
				END AS refresh_interval
			FROM provider_refresh_states prs
			LEFT JOIN entity_access_stats stats ON stats.entity_id = prs.entity_id
			WHERE prs.last_success_at IS NOT NULL
		)
		UPDATE provider_refresh_states prs
		SET next_eligible_at = prs.last_success_at + cadence.refresh_interval
		FROM cadence
		WHERE prs.entity_id = cadence.entity_id AND prs.provider = cadence.provider`)
	if err != nil {
		return fmt.Errorf("recalculate adaptive refresh cadence: %w", err)
	}
	return nil
}

func Cadence(now time.Time, lastAccessed *time.Time, decayedScore float64, scoreUpdatedAt time.Time) time.Duration {
	adjusted := decayedScore
	if !scoreUpdatedAt.IsZero() {
		adjusted *= math.Exp(-now.Sub(scoreUpdatedAt).Seconds() / 604800)
	}
	if (lastAccessed != nil && !lastAccessed.Before(now.Add(-2*24*time.Hour))) || adjusted >= 20 {
		return 2 * 24 * time.Hour
	}
	if (lastAccessed != nil && !lastAccessed.Before(now.Add(-14*24*time.Hour))) || adjusted >= 5 {
		return 7 * 24 * time.Hour
	}
	if lastAccessed != nil && !lastAccessed.Before(now.Add(-60*24*time.Hour)) {
		return 14 * 24 * time.Hour
	}
	return 30 * 24 * time.Hour
}

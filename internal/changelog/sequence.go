// Package changelog sequences transactional outbox entries into the public feed.
package changelog

import (
	"context"
	"log/slog"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
)

const sequencerLock int64 = 0x4845594143484745

// SequenceBestEffort keeps the low-latency after-commit sequencing attempt
// without turning an outbox availability problem into a retry of canonical
// ingestion. The independent River drainer owns eventual delivery.
func SequenceBestEffort(ctx context.Context, runtime *platform.Runtime, limit int) {
	if err := Sequence(ctx, runtime, limit); err != nil {
		slog.WarnContext(ctx, "change outbox sequencing deferred", "error", err)
	}
}

func Sequence(ctx context.Context, runtime *platform.Runtime, limit int) error {
	if limit < 1 {
		limit = 100
	}
	tx, err := runtime.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, sequencerLock); err != nil {
		return err
	}
	var sequence int64
	if err := tx.QueryRow(ctx, `SELECT last_sequence FROM change_cursor WHERE singleton=true FOR UPDATE`).Scan(&sequence); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT id,entity_id,entity_kind,slug,scope,change_type,changed_scopes,projection_version,committed_at FROM change_outbox WHERE sequenced_at IS NULL ORDER BY committed_at,id LIMIT $1 FOR UPDATE SKIP LOCKED`, limit)
	if err != nil {
		return err
	}
	type pending struct {
		id, entityID, kind, slug, scope, changeType string
		scopes                                      []string
		version                                     int64
		at                                          time.Time
	}
	var entries []pending
	for rows.Next() {
		var entry pending
		if err := rows.Scan(&entry.id, &entry.entityID, &entry.kind, &entry.slug, &entry.scope, &entry.changeType, &entry.scopes, &entry.version, &entry.at); err != nil {
			rows.Close()
			return err
		}
		entries = append(entries, entry)
	}
	rows.Close()
	for _, entry := range entries {
		sequence++
		if _, err := tx.Exec(ctx, `INSERT INTO change_log (sequence,outbox_id,entity_id,entity_kind,slug,scope,change_type,changed_scopes,projection_version,created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, sequence, entry.id, entry.entityID, entry.kind, entry.slug, entry.scope, entry.changeType, entry.scopes, entry.version, entry.at); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE change_outbox SET sequenced_at=now() WHERE id=$1`, entry.id); err != nil {
			return err
		}
	}
	if len(entries) > 0 {
		if _, err := tx.Exec(ctx, `UPDATE change_cursor SET last_sequence=$1 WHERE singleton=true`, sequence); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

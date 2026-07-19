// Package workflowfeed sequences async-workflow completions into a gap-free
// cursor feed, mirroring the changelog outbox machinery. The public change
// feed stays focused on canonical projection invalidation; this feed exists so
// a consumer that parked work on a workflow (a discovery run today) learns
// about completion from one cheap poll instead of polling every pending
// workflow individually.
package workflowfeed

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/jackc/pgx/v5/pgconn"
)

const sequencerLock int64 = 0x48455941574f524b

// SequenceBestEffort keeps terminal workflow writes independent from the
// low-latency sequencing attempt. Emit has already staged the durable outbox
// row in the workflow transaction; the River drainer owns eventual delivery.
func SequenceBestEffort(ctx context.Context, runtime *platform.Runtime, limit int) {
	if err := Sequence(ctx, runtime, limit); err != nil {
		slog.WarnContext(ctx, "workflow outbox sequencing deferred", "error", err)
	}
}

// Event is one finished workflow. Kind names the workflow family so more
// families can join the feed without a schema change.
type Event struct {
	Sequence    int64  `json:"sequence"`
	Kind        string `json:"kind" enum:"discovery"`
	ID          string `json:"id" format:"uuid"`
	State       string `json:"state" enum:"completed,failed"`
	CompletedAt string `json:"completed_at"`
}

type Page struct {
	StreamID  string
	Head      int64
	Events    []Event
	Next      int64
	QueryTime time.Duration
}

type CursorConflict struct {
	Code     string
	StreamID string
	Head     int64
}

func (conflict *CursorConflict) Error() string {
	switch conflict.Code {
	case "workflow_stream_changed":
		return "the workflow event stream identity differs from the client stream"
	default:
		return "the workflow event cursor is ahead of the available stream"
	}
}

type execer interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
}

// Emit stages a completion in the caller's transaction so the event commits
// atomically with the workflow's terminal state.
func Emit(ctx context.Context, tx execer, kind, workflowID, state string, completedAt time.Time) error {
	_, err := tx.Exec(ctx, `INSERT INTO workflow_event_outbox (workflow_kind,workflow_id,state,completed_at) VALUES ($1,$2,$3,$4)`, kind, workflowID, state, completedAt)
	if err != nil {
		return fmt.Errorf("stage workflow event: %w", err)
	}
	return nil
}

// Sequence assigns gap-free sequence numbers to staged events under an
// advisory lock, exactly like changelog.Sequence does for entity changes.
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
	if err := tx.QueryRow(ctx, `SELECT last_sequence FROM workflow_event_cursor WHERE singleton=true FOR UPDATE`).Scan(&sequence); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT id,workflow_kind,workflow_id::text,state,completed_at FROM workflow_event_outbox WHERE sequenced_at IS NULL ORDER BY committed_at,id LIMIT $1 FOR UPDATE SKIP LOCKED`, limit)
	if err != nil {
		return err
	}
	type pending struct {
		id, kind, workflowID, state string
		completedAt                 time.Time
	}
	var entries []pending
	for rows.Next() {
		var entry pending
		if err := rows.Scan(&entry.id, &entry.kind, &entry.workflowID, &entry.state, &entry.completedAt); err != nil {
			rows.Close()
			return err
		}
		entries = append(entries, entry)
	}
	rows.Close()
	for _, entry := range entries {
		sequence++
		if _, err := tx.Exec(ctx, `INSERT INTO workflow_event_log (sequence,outbox_id,workflow_kind,workflow_id,state,completed_at) VALUES ($1,$2,$3,$4,$5,$6)`, sequence, entry.id, entry.kind, entry.workflowID, entry.state, entry.completedAt); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE workflow_event_outbox SET sequenced_at=now() WHERE id=$1`, entry.id); err != nil {
			return err
		}
	}
	if len(entries) > 0 {
		if _, err := tx.Exec(ctx, `UPDATE workflow_event_cursor SET last_sequence=$1 WHERE singleton=true`, sequence); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// ReadPage reads a stable page bounded by the head observed at the start of
// the request, with the same stream-identity and cursor-ahead validation the
// change feed uses.
func ReadPage(ctx context.Context, runtime *platform.Runtime, knownStreamID string, after int64, limit int) (Page, error) {
	if limit < 1 || limit > 500 {
		limit = 100
	}
	started := time.Now()
	page := Page{Events: []Event{}, Next: after}
	if err := runtime.DB.QueryRow(ctx, `
		SELECT cursor.stream_id::text,
		       COALESCE((SELECT max(log.sequence) FROM workflow_event_log log),0)
		FROM workflow_event_cursor cursor
		WHERE cursor.singleton=true`).Scan(&page.StreamID, &page.Head); err != nil {
		return Page{}, fmt.Errorf("read workflow event stream metadata: %w", err)
	}
	if err := validateCursor(knownStreamID, page.StreamID, after, page.Head); err != nil {
		return Page{}, err
	}

	rows, err := runtime.DB.Query(ctx, `
		SELECT sequence,workflow_kind,workflow_id::text,state,completed_at
		FROM workflow_event_log
		WHERE sequence > $1 AND sequence <= $2
		ORDER BY sequence
		LIMIT $3`, after, page.Head, limit)
	if err != nil {
		return Page{}, fmt.Errorf("read workflow event page: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var event Event
		var completedAt time.Time
		if err := rows.Scan(&event.Sequence, &event.Kind, &event.ID, &event.State, &completedAt); err != nil {
			return Page{}, fmt.Errorf("scan workflow event: %w", err)
		}
		event.CompletedAt = completedAt.UTC().Format(time.RFC3339Nano)
		page.Events = append(page.Events, event)
		page.Next = event.Sequence
	}
	if err := rows.Err(); err != nil {
		return Page{}, fmt.Errorf("iterate workflow events: %w", err)
	}
	page.QueryTime = time.Since(started)
	return page, nil
}

func validateCursor(knownStreamID, streamID string, after, head int64) error {
	if knownStreamID != "" && knownStreamID != streamID {
		return &CursorConflict{Code: "workflow_stream_changed", StreamID: streamID, Head: head}
	}
	if after > head {
		return &CursorConflict{Code: "workflow_cursor_ahead", StreamID: streamID, Head: head}
	}
	return nil
}

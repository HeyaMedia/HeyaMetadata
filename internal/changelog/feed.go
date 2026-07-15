package changelog

import (
	"context"
	"fmt"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
)

// ChangeEntry is one durable public projection change. Provider observations and
// private maintenance events never appear in this feed.
type ChangeEntry struct {
	Sequence          int64    `json:"sequence"`
	EntityID          string   `json:"entity_id" format:"uuid"`
	EntityKind        string   `json:"entity_kind"`
	Slug              string   `json:"slug"`
	ChangeType        string   `json:"change_type"`
	ChangedScopes     []string `json:"changed_scopes"`
	ProjectionVersion int64    `json:"projection_version"`
	CreatedAt         string   `json:"created_at"`
}

type Page struct {
	StreamID  string
	Head      int64
	Entries   []ChangeEntry
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
	case "change_stream_changed":
		return "the change stream identity differs from the client stream"
	default:
		return "the change cursor is ahead of the available stream"
	}
}

// ReadPage reads a stable page bounded by the head observed at the beginning
// of the request. A concurrent publication is therefore picked up on the next
// request instead of making next_cursor exceed the returned head_cursor.
func ReadPage(ctx context.Context, runtime *platform.Runtime, knownStreamID string, after int64, limit int) (Page, error) {
	if limit < 1 || limit > 500 {
		limit = 100
	}
	started := time.Now()
	page := Page{Entries: []ChangeEntry{}, Next: after}
	if err := runtime.DB.QueryRow(ctx, `
		SELECT cursor.stream_id::text,
		       COALESCE((SELECT max(log.sequence) FROM change_log log WHERE log.scope='public'),0)
		FROM change_cursor cursor
		WHERE cursor.singleton=true`).Scan(&page.StreamID, &page.Head); err != nil {
		return Page{}, fmt.Errorf("read change stream metadata: %w", err)
	}
	if err := validateCursor(knownStreamID, page.StreamID, after, page.Head); err != nil {
		return Page{}, err
	}

	rows, err := runtime.DB.Query(ctx, `
		SELECT sequence,entity_id,entity_kind,slug,change_type,changed_scopes,projection_version,created_at
		FROM change_log
		WHERE sequence > $1 AND sequence <= $2 AND scope='public'
		ORDER BY sequence
		LIMIT $3`, after, page.Head, limit)
	if err != nil {
		return Page{}, fmt.Errorf("read public change page: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var entry ChangeEntry
		var createdAt time.Time
		if err := rows.Scan(&entry.Sequence, &entry.EntityID, &entry.EntityKind, &entry.Slug, &entry.ChangeType, &entry.ChangedScopes, &entry.ProjectionVersion, &createdAt); err != nil {
			return Page{}, fmt.Errorf("scan public change: %w", err)
		}
		entry.CreatedAt = createdAt.UTC().Format(time.RFC3339Nano)
		page.Entries = append(page.Entries, entry)
		page.Next = entry.Sequence
	}
	if err := rows.Err(); err != nil {
		return Page{}, fmt.Errorf("iterate public changes: %w", err)
	}
	page.QueryTime = time.Since(started)
	return page, nil
}

func validateCursor(knownStreamID, streamID string, after, head int64) error {
	if knownStreamID != "" && knownStreamID != streamID {
		return &CursorConflict{Code: "change_stream_changed", StreamID: streamID, Head: head}
	}
	if after > head {
		return &CursorConflict{Code: "change_cursor_ahead", StreamID: streamID, Head: head}
	}
	return nil
}

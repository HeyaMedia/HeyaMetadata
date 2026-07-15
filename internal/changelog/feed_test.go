package changelog

import (
	"errors"
	"testing"
)

func TestValidateCursorAcceptsEmptyAndCurrentHead(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name  string
		after int64
		head  int64
	}{
		{name: "empty stream", after: 0, head: 0},
		{name: "at head", after: 42, head: 42},
		{name: "behind head", after: 10, head: 42},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if err := validateCursor("stream-a", "stream-a", test.after, test.head); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestValidateCursorRejectsAheadAndChangedStreams(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, known, current, code string
		after, head                int64
	}{
		{name: "cursor ahead", known: "stream-a", current: "stream-a", after: 43, head: 42, code: "change_cursor_ahead"},
		{name: "stream changed", known: "stream-old", current: "stream-new", after: 2, head: 42, code: "change_stream_changed"},
		{name: "stream change outranks cursor", known: "stream-old", current: "stream-new", after: 100, head: 42, code: "change_stream_changed"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var conflict *CursorConflict
			if err := validateCursor(test.known, test.current, test.after, test.head); !errors.As(err, &conflict) {
				t.Fatalf("error = %v", err)
			}
			if conflict.Code != test.code || conflict.StreamID != test.current || conflict.Head != test.head {
				t.Fatalf("conflict = %+v", conflict)
			}
		})
	}
}

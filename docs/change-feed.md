# Public change feed

`GET /api/v2/changes` is the durable invalidation stream for canonical public
projections. Consumers use Heya UUIDs and projection versions from this feed;
provider observations and private maintenance events are not exposed.

## Stream identity and cursors

`change_cursor.stream_id` is a singleton UUID generated when a database is
initialized. Migrations and process restarts preserve it. A new database gets a
new UUID, while restoring a snapshot restores the snapshot's UUID and head.

Each successful response contains:

```json
{
  "stream_id": "6e53b69c-d158-46a5-913d-6e4a5401bcf8",
  "head_cursor": 32554,
  "entries": [],
  "next_cursor": 32554
}
```

`head_cursor` is the highest available public sequence, or zero when the stream
is empty. A page is bounded by the head observed at the start of that request,
so concurrent publication cannot make its `next_cursor` exceed its reported
head. A cursor equal to the head is valid and returns an empty `entries` array.

The first request may omit `stream_id`. Consumers persist the returned value
with `next_cursor` and supply both thereafter:

```http
GET /api/v2/changes?after=32554&limit=500&stream_id=6e53b69c-d158-46a5-913d-6e4a5401bcf8
```

Apply entries in sequence order and commit the new cursor only after all local
effects commit. Applying an entry and replaying a page must both be idempotent.

## Reset recovery

The endpoint returns `409 application/problem+json` instead of an empty page
when continuing would be unsafe:

```json
{
  "type": "https://heya.media/problems/change_cursor_ahead",
  "title": "Conflict",
  "status": 409,
  "detail": "the change cursor is ahead of the available stream",
  "code": "change_cursor_ahead",
  "stream_id": "6e53b69c-d158-46a5-913d-6e4a5401bcf8",
  "head_cursor": 32554
}
```

- `change_stream_changed`: the supplied stream UUID is not the current UUID,
  normally because the database was rebuilt.
- `change_cursor_ahead`: the cursor is above the current public head, normally
  because an older database snapshot was restored.

For either conflict, the consumer adopts the returned `stream_id`, resets its
cursor to zero, and replays idempotently. It must not clamp the old cursor to the
new head because doing so would skip changes needed to rebuild local state.

## Operational visibility

Responses are never cached. `Server-Timing: changes;dur=...` reports origin
query time, and structured logs include the stream ID, requested cursor, head,
next cursor, returned entry count, and duration. Large JSON responses support
gzip content negotiation through the normal API response path.

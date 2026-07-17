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

# Workflow completion feed

`GET /api/v2/workflow-events` is the companion stream for async workflows.
A discovery run can finish (with candidates, or by failing) without updating
any canonical entity, so `/api/v2/changes` never announces it. Consumers that
parked work on a workflow — for example a media server waiting on thousands of
discovery runs — poll this feed with one cheap cursor request instead of
polling every pending workflow individually.

Events carry the workflow family, its public id, and the terminal state:

```json
{
  "stream_id": "74c99c9b-b605-4c57-82d2-abe6aec22a09",
  "head_cursor": 1234,
  "events": [
    {
      "sequence": 1234,
      "kind": "discovery",
      "id": "122ca081-208f-4031-be0e-20328769c8c4",
      "state": "completed",
      "completed_at": "2026-07-17T17:40:23.384155Z"
    }
  ],
  "next_cursor": 1234
}
```

The stream has its own identity and cursor (`workflow_event_cursor`), and the
same rules as the change feed apply verbatim: pages are bounded by the head
observed at request start, cursors persist alongside local effects, and `409`
conflicts (`workflow_stream_changed`, `workflow_cursor_ahead`) require adopting
the returned `stream_id`, resetting to cursor zero, and replaying idempotently.
Events are emitted transactionally with the workflow's terminal state, so a
completion observed through `GET /api/v2/discoveries/{id}` is always visible in
the feed once its sequence is assigned. `kind` currently emits `discovery`;
new workflow families may appear and consumers must ignore kinds they do not
recognize.

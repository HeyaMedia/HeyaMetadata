# Canonical entity lookup and resolution flow

This document is the integration contract for a server consuming HeyaMetadata.
The client always ends with a Heya canonical entity ID and reads the combined
document from `GET /api/v2/entities/{id}`. Provider IDs are discovery and
ingestion inputs; they are not the identity exposed to the rest of Heya.

## End-to-end flow

```text
GET /api/v2/search?q=...&kind=...
  |
  +-- result selected
  |     `-- GET /api/v2/entities/{entity_id}
  |
  `-- no acceptable result
        `-- POST /api/v2/discoveries
              |
              +-- 202 queued/working
              |     `-- poll GET /api/v2/discoveries/{discovery_id}
              |
              `-- 200 completed
                    `-- select a candidate
                          |
                          +-- candidate.existing_entity_id is present
                          |     `-- GET /api/v2/entities/{existing_entity_id}
                          |
                          `-- POST candidate.resolution to /api/v2/resolutions
                                |
                                +-- 200 completed
                                |     `-- use entity_id (entity is also embedded)
                                |
                                `-- 202 accepted
                                      `-- poll GET /api/v2/jobs/{job_id}
                                            `-- use entity_id when available

GET /api/v2/entities/{entity_id}
```

There are deliberately three distinct operations:

- **Search** reads only the local canonical Postgres/Redis index. It never
  waits on an upstream provider and is the normal fast path.
- **Discovery** searches upstream identity providers and ranks candidates. It
  does not create, merge, or select a canonical entity.
- **Resolution** confirms one provider identity and idempotently resolves or
  ingests it into the combined canonical model.

## 1. Search the canonical index

```http
GET /api/v2/search?q=ano&kind=artist&limit=20
```

Optional filters currently include `year`, `genre`, `country`, `language`, and
`status`. `kind` should be supplied whenever the caller knows it. Valid search
kinds are `movie`, `artist`, `release_group`, `tv_show`, and `anime`; TV and
Anime remain separate domains.

If the caller can confidently select a result, read its `entity_id` and skip
discovery. An empty list, or results that do not satisfy the caller's identity
hints, falls through to discovery. A fuzzy title/name hit alone must not be
treated as a durable identity match.

## 2. Discover upstream candidates

Artist discovery is the first implemented provider-backed route. Other kinds
are part of the contract but currently return `400` until their provider
routing is implemented.

```http
POST /api/v2/discoveries
Content-Type: application/json
Prefer: wait=5

{
  "kind": "artist",
  "query": "ano",
  "limit": 10,
  "hints": {
    "country": "JP",
    "type": "person",
    "aliases": ["あの"],
    "releases": [
      {"title": "猫猫吐吐", "year": 2023}
    ]
  }
}
```

The server waits for 1.2 seconds by default. `Prefer: wait=N` requests a wait
of up to five seconds, while `Prefer: respond-async` returns immediately. A
completed response is `200`; a `202` response contains an `id` and a
`queued`/`working` state. Poll:

```http
GET /api/v2/discoveries/{id}
```

until `state` is `completed` or `failed`. Identical normalized requests reuse
the same durable work and completed discovery result for six hours.

The completed result contains a recommendation and ranked candidates. Each
candidate includes explainable evidence, confidence, an optional
`existing_entity_id`, and a `resolution` object shaped exactly for the next
request:

```json
{
  "kind": "artist",
  "provider": "musicbrainz",
  "namespace": "artist",
  "value": "ebb4513e-4aab-4ac9-a949-14e77bb7b836"
}
```

The consuming server owns the final selection policy. `strong_match` can be
automated when its evidence meets product policy; `ambiguous` should normally
be returned to the user for selection or retried with better hints. Never
silently select a weak first result merely because it has rank 1.

If the chosen candidate already has `existing_entity_id`, the client may read
that entity immediately. Posting its resolution is also safe, but unnecessary.

## 3. Resolve the selected provider identity

Submit the selected candidate's `resolution` object without reconstructing it:

```http
POST /api/v2/resolutions
Content-Type: application/json

{
  "kind": "artist",
  "provider": "musicbrainz",
  "namespace": "artist",
  "value": "ebb4513e-4aab-4ac9-a949-14e77bb7b836"
}
```

If the identity is already canonical, the response is `200` with
`state: "completed"`, `entity_id`, and the combined `entity` document. If it
must be ingested, the response is `202` with `state: "accepted"` and a durable
`job`. Poll:

```http
GET /api/v2/jobs/{job.id}
```

When the job response contains `entity_id`, ingestion has produced the
canonical entity and the client can read it. A terminal job with `error` is a
failed resolution and must not be cached as a successful lookup.

Resolution is idempotent at the canonical identity/job level. Clients may
retry the same request after timeouts using normal bounded backoff; they must
not invent a second local identity while the first resolution is in flight.

## 4. Read and retain the canonical identity

```http
GET /api/v2/entities/{entity_id}
```

Store the Heya `entity_id` as the durable reference in the consuming server.
The returned document is the single combined view assembled from all eligible
providers. Do not persist one provider's response as a competing canonical
record.

An entity read may return data marked stale while HeyaMetadata schedules a
lower-priority refresh. Stale-while-revalidate is intentional: callers should
use the returned canonical document instead of restarting discovery.

## Provider credentials

User-supplied provider credentials may be forwarded on resolution and entity
requests using the documented `X-Heya-*-API-Key` headers. They are handed to
workers through short-lived opaque references and are never written to River
or Postgres. The OpenAPI document is authoritative for the supported headers.

## Client implementation rules

1. Search locally first and always include the known kind.
2. Fall through to discovery only when no acceptable canonical result exists.
3. Preserve the discovery ID and resolution job ID so requests survive client
   restarts and HTTP timeouts.
4. Use bounded exponential backoff with jitter when polling; honor `Retry-After`
   if the API adds it. Do not poll in a tight loop.
5. Pass `candidate.resolution` through unchanged.
6. Persist and expose only the resulting Heya `entity_id` as canonical identity.
7. Do not collapse `tv_show` and `anime`, or provider IDs and Heya IDs.
8. Treat discovery ranking as evidence for identity selection, not metadata to
   merge into the final entity.


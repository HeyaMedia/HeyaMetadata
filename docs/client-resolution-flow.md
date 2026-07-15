# Provider-transparent canonical identity flow

This is the identity contract for every HeyaMetadata client. HeyaMetadata is
the only metadata provider from the client's point of view. TMDB, IMDb, TVDB,
MusicBrainz, AniDB, ISBNs, and every other external identifier are evidence
given to HeyaMetadata; they are never client-side routing instructions.

The durable identity returned to a client is always a Heya UUID.

## End-to-end flow

```text
identifiers present?
  |
  +-- yes -> POST /api/v2/discoveries with every identifier + facts
  |
  `-- no  -> GET /api/v2/search?q=...&kind=...
              |
              +-- acceptable local result -> retain result.id
              `-- miss -> POST /api/v2/discoveries with query + hints

completed discovery
  |
  +-- result.entity_id -> retain the Heya UUID and read the entity directly
  |
  `-- result.status = needs_selection
        `-- show/select ranked candidate
              `-- POST {"candidate_ref":"..."} to /api/v2/resolutions
                    +-- completed: retain entity_id
                    `-- accepted: poll /api/v2/jobs/{job.id}
```

There are three identity operations:

- **Search** is the fast local-only path for query-only matching over already
  canonical data.
- **Discovery** accepts all available identifiers and descriptive facts. It
  resolves known claims locally, crosswalks fresh identifiers internally, or
  searches upstream when necessary.
- **Resolution** selects one opaque candidate. Only HeyaMetadata can interpret
  the reference and complete the selected canonical identity.

Resolution is optional. It is not called when discovery already returns
`result.entity_id`.

The client never selects an upstream provider, reconstructs a provider
namespace, or decides which external identifier drives private ingestion.

## 1. Search locally first only for query-only matching

```http
GET /api/v2/search?q=Ado&kind=artist&limit=20
```

Search never calls an upstream service. A selected result's `id` is a Heya
UUID and can be passed directly to `GET /api/v2/entities/{id}`. Include `kind`
whenever it is known. A fuzzy name/title hit alone is not durable identity; if
the result does not satisfy the caller's evidence, continue to discovery.

If any external identifiers are available, skip local search and submit every
identifier directly to discovery. This lets HeyaMetadata verify known and fresh
evidence together before fuzzy title/name matching.

## 2. Give discovery all available evidence

`query` is optional when at least one identifier is present. Identifier-only
discovery is a required path for media scans with good embedded IDs.

```http
POST /api/v2/discoveries
Content-Type: application/json
Prefer: wait=5

{
  "kind": "tv_show",
  "identifiers": [
    {"scheme": "tmdb", "value": "1396"},
    {"scheme": "imdb", "value": "tt0903747"},
    {"scheme": "tvdb", "value": "81189"}
  ],
  "query": "Breaking Bad",
  "hints": {
    "year": 2008,
    "episodes": [{"season": 1, "number": 1}]
  }
}
```

Send every identifier available from NFOs, tags, filenames, URLs, or existing
rows. Identifier ordering does not affect request deduplication. Unknown or
unsupported identifiers do not invalidate otherwise useful evidence.

Supported identifier evidence outranks fuzzy title/name matching. When one
identifier is already known locally and another supported identifier is fresh,
HeyaMetadata does not return the known entity early: the durable worker
crosswalks the fresh evidence and verifies convergence. Agreement returns the
single existing Heya UUID; disagreement returns opaque reviewable candidates.
Only when identifier evidence cannot establish an identity may the query and
hints drive fuzzy upstream discovery.

For artists, submit MusicBrainz, Apple/iTunes, Deezer, and any known release
identifiers together. HeyaMetadata privately ingests each supported artist root
and follows MusicBrainz release credits. It returns a canonical Heya UUID only
when that evidence converges; genuine disagreement remains an opaque selection.
An explicit MusicBrainz relationship may consolidate duplicate storefront roots,
but a matching artist name alone never does.

The generic route supports `movie`, `tv_show`, `anime`, `artist`,
`release_group`, `recording`, `musical_work`, `book_work`, `manga`,
`manga_volume`, and `comic_volume`. Dedicated TV, anime, manga, manga-volume,
and comic discovery routes are conveniences that fix the kind; they do not
change the identity model.

Responses use the durable discovery resource:

- `200` when local identity resolution or short upstream work completed;
- `202` with `state: queued|working` when work continues in River;
- `503` with `Retry-After` only after all worker attempts fail; resubmitting the
  same normalized request safely resumes it;
- poll `GET /api/v2/discoveries/{id}` until `completed` or `failed`.

A completed result can be one of two shapes.

### Unique identity

```json
{
  "status": "completed",
  "entity_id": "2dd16bc3-dfaa-47b0-bf96-12dc63e47fd7",
  "identifier_evidence": [
    {"scheme": "tmdb", "value": "1396", "outcome": "resolved"},
    {"scheme": "imdb", "value": "tt0903747", "outcome": "corroborating"}
  ]
}
```

Retain `entity_id`; no resolution call is needed.

Query-and-hint discovery also returns this shape when its decisively ranked
upstream candidate already has an accepted external-ID claim on a canonical
entity. This is claim reconciliation, not title/year deduplication: a fuzzy,
weak, or ambiguous result remains `needs_selection`, including genuinely
different productions that share a title and year.

### Ambiguous or conflicting identity

```json
{
  "status": "needs_selection",
  "candidates": [
    {
      "candidate_ref": "7edb3b9e-0ae8-4ac8-8735-37046f67aa2d",
      "confidence": 0.92,
      "match": "strong",
      "display": {"title": "Example", "year": 2024},
      "evidence": []
    }
  ]
}
```

Candidates expose display and matching evidence, but no provider identity,
existing-entity shortcut, or provider-shaped resolution object. A conflict is
never silently merged.

`identifier_evidence[].outcome` is one of:

- `resolved`: this identifier established the unique canonical entity;
- `corroborating`: it independently agrees with that entity;
- `unused`: valid evidence that did not produce a local claim or route;
- `unsupported`: HeyaMetadata does not currently interpret the scheme;
- `conflict`: it resolves to a different canonical entity.

## 3. Select by opaque candidate reference only when required

Do not call this operation when discovery returns `result.entity_id`; proceed
directly to the canonical read. Resolution exists only for a selected candidate
from a `needs_selection` result.

```http
POST /api/v2/resolutions
Content-Type: application/json

{
  "candidate_ref": "7edb3b9e-0ae8-4ac8-8735-37046f67aa2d"
}
```

The reference is short-lived and owned by HeyaMetadata. The client must not
cache it as identity or decode/reconstruct the provider route it represents.

A known candidate returns `200` with `state: completed`, `entity_id`, and the
combined entity. A candidate requiring ingestion returns `202` with
`state: accepted` and a River job. Poll `GET /api/v2/jobs/{job.id}`; retain its
`entity_id` once available. Retry timeouts with bounded backoff. Candidate
selection and ingestion are idempotent and concurrent retries must converge on
the same canonical entity.

Fingerprint matching follows the same boundary. A materialized match carries
`entity_id`; an upstream-only match carries `candidate_ref`. It never returns a
MusicBrainz ID or provider-shaped resolution request for client control flow.

## 4. Canonical relation closure

Every independently addressable resource is a Heya UUID. Nested data either
contains the target Heya ID or says explicitly that the target is not yet
materialized.

| Data | Canonical reference exposed to clients |
| --- | --- |
| Movie, TV show, anime, artist, release group, release, recording, musical work, book work/edition, manga, manga/comic volume, author, person | top-level `id` / `entity_id` |
| Issued-edition track placement | `data.media[].tracks[].id` |
| Reusable song/recording | `recording_entity_id` |
| Track or release artist credit | `artist_entity_id` |
| Release-group edition | `data.editions[].entity_id` |
| Recording appearance | `release_entity_id` and `release_group_entity_id` |
| Cast/crew member | `person_entity_id` |
| Person filmography item | `entity_id` |
| TV/anime season | `data.seasons[].id` |
| TV/anime episode | `data.episodes[].id` and `season_id` |
| Recommendation | `entity_id` |
| Movie collection/franchise | collection `id`; each materialized member has `entity_id` |
| Book author and edition | author `id`, edition `id`; an edition's `work_id` points back to its work |
| Generic entity relation | `target_entity_id` |

Where a provider reports a related object that has not been canonicalized, the
relationship has `resolution_state: unresolved` and omits the Heya target ID.
When present, `resolution_state: materialized` guarantees that the Heya target
ID is the navigable identity. Clients must not fall back to provider IDs when a
relation is unresolved.

Some nested objects are values rather than identities: a character name,
director job, disc number, genre, rating, label credit, and similar attributes
do not receive IDs merely for appearing in metadata.

## 5. Read only by Heya ID

The generic combined read is:

```http
GET /api/v2/entities/{id}
```

Canonical follow-up reads also take Heya IDs:

- `/api/v2/entities/{id}/credits`
- `/api/v2/entities/{id}/ratings`
- `/api/v2/entities/{id}/images`
- `/api/v2/entities/{id}/relations`
- `/api/v2/entities/{id}/top-tracks`
- `/api/v2/entities/{id}/refreshes`
- `/api/v2/persons/{id}` and `/api/v2/persons/{id}/credits`
- `/api/v2/tv/shows/{id}`, `/api/v2/anime/{id}`
- `/api/v2/seasons/{id}`, `/api/v2/episodes/{id}`
- `/api/v2/recordings/{id}`, `/api/v2/releases/{id}`
- `/api/v2/collections/{id}`
- publication and manga detail routes documented in OpenAPI.

External IDs may remain in `external_ids`, provenance, links, or passive source
fields so users can inspect and visit upstream records. Their presence does
not authorize the client to route a request using them.

## Provider credentials

Request-scoped provider credentials can be forwarded using the documented
`X-Heya-*-API-Key` headers. HeyaMetadata passes them to workers through
short-lived opaque references and never persists them in River or Postgres.
The OpenAPI document is authoritative for supported headers.

## Client rules

1. If any identifiers exist, send kind, every identifier, and all useful local
   facts directly to discovery.
2. For query-only matching, search locally first; on a miss, send the query and
   hints to discovery.
3. Persist discovery/job IDs so polling survives client restarts.
4. Use only `entity_id` as durable identity and only `candidate_ref` for an
   ambiguity selection.
5. Never construct provider namespaces or provider-shaped resolution bodies.
6. Never use passive external IDs to decide a follow-up endpoint.
7. Honor `resolution_state`; unresolved means displayable evidence, not a
   navigable identity.
8. Keep `tv_show` and `anime`, release groups and editions, recordings and
   track placements as distinct canonical concepts.

# Heya Metadata v2

## Status

This document defines the intended architecture for the next Heya metadata
server. V2 is a clean-slate implementation with no storage, wire-format,
endpoint, identifier, or behavioral compatibility requirement with the current
server.

The current metadata server, frontend, and Heya media server are references for
metadata coverage, provider knowledge, and known edge cases only. They do not
define the v2 data model or API. All clients can change with the server because
they are controlled together.

The decisions in this document are deliberate:

- Postgres owns durable operational and canonical state.
- RustFS at `s3.karbowiak.dk` owns immutable payload and artifact blobs.
- Redis is present from the first iteration for shared cache and coordination.
- River is the durable job system. Kafka is not part of this architecture.
- Postgres trigram and full-text indexes provide search. Meilisearch is not part
  of v2.
- Dedicated workers perform provider traffic, imports, projection builds, and
  media analysis. API nodes do not own background work.
- The API is designed from the v2 domain model. Heya and the web frontend adapt
  to it.
- V2 must expose at least the same useful metadata and source coverage as the
  current system, but not in the same document shape.

## Why v2 exists

The current server has established the information and product capabilities we
want:

- Search across several metadata providers.
- Resolve an external ID or stable slug to an enriched entity.
- Make rich normalized metadata efficient to consume without requiring one
  legacy document shape.
- Expose localized titles, overviews, artwork, credits, external IDs, and
  provider success or failure state.
- Serve stale data while refreshing it in the background.
- Deduplicate work, prioritize interactive requests, recover abandoned jobs,
  and respect provider rate limits.
- Materialize images lazily into S3-compatible storage.

Its implementation is less suitable for the intended scale. Canonical data,
serving documents, queue state, cached provider responses, search state, image
state, and future user submissions have different lifecycles and access
patterns. V2 gives each one an explicit home.

V2 is also the point at which raw provider responses become durably archived.
The old database is not a required input to v2 and is not treated as a complete
raw-source archive.

## Goals

- Give every entity an immutable internal identity independent of providers and
  slugs.
- Retain exact upstream observations so merge and normalizer changes can be
  replayed without refetching providers.
- Make provider fetches, imports, merges, projections, submissions, and edits
  safe to retry.
- Keep provenance from raw response through normalized source data to the
  public projection.
- Make every useful metadata field available through a coherent, versioned API.
- Serve API projections quickly from Postgres and Redis.
- Scale API and worker nodes independently.
- Make changes discoverable to Heya media servers through a durable cursor-based
  feed.
- Accept authenticated media analysis and, later, moderated metadata edits.
- Keep storage growth bounded and visible within the initial 1 TB RustFS
  allocation.

## Metadata and provider coverage

V2 starts with a coverage inventory, not an API compatibility suite. The
inventory records every useful field, relationship, locale, image class, and
artifact the current product can obtain, which providers supply it, and the
identity assumptions involved.

Initial source coverage includes at least:

- Video and people: TMDB, TVDB, OMDb, AniDB, TVMaze, Fanart.tv, TheXEM, and the
  anime-list mapping data.
- Music: MusicBrainz, Discogs, Deezer, Apple, Last.fm, ListenBrainz,
  TheAudioDB, Fanart.tv, Wikidata, OpenOpus, and YouTube where appropriate.
- Books and audiobooks: OpenLibrary, Google Books, Audible-derived data, and
  Wikidata where useful.
- Segments: TheIntroDB, SkipMe.db, and AniSkip.

This is a floor, not a closed list. New providers are welcome when their terms,
data quality, rate limits, and identity semantics are understood. A v2 feature
is complete when its intended metadata is available with provenance; it does
not need to reproduce the old JSON path that carried it.

## Non-goals

- Kafka, event sourcing, or a general-purpose streaming platform.
- A graph database. The external-ID and relation graph belongs in Postgres.
- Storing every source-shaped payload inline in Postgres JSONB.
- Normalizing every leaf value into an EAV table.
- Replacing RustFS or Redis with Postgres merely to reduce the component count.
- Backward compatibility with old endpoints, payloads, slugs, image keys,
  status codes, generated clients, or databases.
- Importing the old Mongo or Meilisearch data as a prerequisite for launch.
- Community editing, voting, or ML pipelines in the first usable release.

## System architecture

```text
                         +---------------------+
                         | Heya / web frontend |
                         +----------+----------+
                                    |
                              public API
                                    |
                   +----------------v----------------+
                   | stateless API/frontend nodes    |
                   | auth, reads, search, job submit |
                   +------+-------------+------------+
                          |             |
                  cache / locks         | durable reads/writes
                          |             |
                   +------v------+  +---v-----------------------+
                   | Redis       |  | Postgres                  |
                   | volatile    |  | canonical + operational   |
                   +------+------+  +---+-----------------------+
                          |             |
                     notifications      | River jobs
                          |             |
                   +------v-------------v------+
                   | dedicated worker pools    |
                   | fetch/import/merge/analyze|
                   +-------------+-------------+
                                 |
                         immutable blobs
                                 |
                   +-------------v-------------+
                   | RustFS / s3.karbowiak.dk  |
                   | raw payloads + artifacts  |
                   +---------------------------+
```

### Postgres

Postgres is the durable operational brain. It owns:

- Canonical entities and their lifecycle.
- External identifier claims and identity decisions.
- Entity relations.
- Versioned normalized source records and merge state.
- Rebuildable API documents and search projections.
- River jobs, refresh state, and provider scheduling state.
- Users, roles, sessions, API keys, quotas, and audit logs.
- Revisions, moderation, and field overrides.
- Blob references, fetch observations, artifact indexes, and retention state.
- The durable change log and consumer cursors where server-side cursor tracking
  is useful.

Large source payloads and analysis artifacts are referenced from Postgres, not
stored inline.

### RustFS object storage

The initial blob store is the RustFS service at `s3.karbowiak.dk`, with roughly
1 TB available. It owns:

- Compressed raw provider response bodies.
- Provider dumps, snapshots, and import manifests.
- Image originals and generated image variants where retained.
- Chromaprints and other large audio-analysis payloads.
- Embeddings, waveform summaries, subtitle or OCR output, and model-versioned
  artifacts.
- Large diagnostic or detailed-diff objects that do not belong on a hot SQL
  path.

Stored bytes are content-addressed. A representative internal key layout is:

```text
blobs/sha256/ab/cd/<checksum>.zst
artifacts/<kind>/<tool-version>/sha256/ab/cd/<checksum>.zst
dumps/<provider>/<snapshot-version>/<object>
images/original/sha256/ab/cd/<checksum>
images/derived/<transform-version>/sha256/ab/cd/<checksum>.webp
```

The exact path is an implementation detail; the checksum and metadata in
Postgres are authoritative. Server-side encryption, bucket versioning policy,
multipart cleanup, lifecycle rules, and restore testing must be configured
before production data depends on the bucket.

Content identity is not public image identity. Each normalized image candidate
receives an opaque internal image ID before its bytes are fetched. API documents
reference that ID, and an image record retains the source URL, provider,
language, dimensions, scores, and other provenance needed for lazy
materialization.

On first request, a worker fetches the source, validates it, writes or reuses a
content-addressed blob, generates bounded serving variants, and updates the
image record. Public image routes use the opaque ID plus a declared transform,
not an upstream URL or a legacy URL hash. Unknown image IDs fail without turning
the service into an arbitrary URL proxy.

Cast and crew profile images default to a derived serving format only; their
originals are not retained. Other artwork keeps originals only when its
retention class and source license justify the cost.

### Redis

Redis is part of the first iteration. It is shared infrastructure, not an
optional later optimization.

It owns volatile state only:

- API document and search-result caches shared by all API nodes.
- Negative caches for safe, short-lived upstream misses.
- Cached decompressed or normalized reads of hot RustFS blobs.
- Cross-node singleflight locks such as `lock:enrich:<kind>:<entity-id>`.
- Provider rate-limit buckets and concurrency leases shared by all workers.
- Per-user and per-key API rate limits.
- Short-lived River job completion notifications.
- Cache invalidation messages after projection commits.

Every Redis key is namespaced and versioned. Caches have explicit TTLs and may
be deleted at any time. Locks use unique ownership tokens and bounded leases;
unlock is compare-and-delete. Pub/sub is only a wake-up mechanism: Postgres
always contains the durable state a reconnecting process needs.

Redis failure may reduce performance or temporarily prevent rate-limited
provider work, but it must not corrupt canonical data. Provider workers fail
closed when a shared quota cannot be enforced safely.

### Dedicated workers and River

All durable background work is represented as River jobs in Postgres. API
processes may enqueue work and briefly wait for it, but do not execute provider
pipelines in request-owned goroutines.

Initial job kinds include:

- `fetch_entity`
- `search_provider`
- `refresh_provider_record`
- `normalize_provider_record`
- `merge_entity`
- `rebuild_api_document`
- `rebuild_search_projection`
- `import_source_dump`
- `reconcile_external_ids`
- `process_fingerprint`
- `process_analysis_artifact`
- `backfill_blob_metadata`
- `apply_retention_policy`

Jobs carry versioned arguments and deterministic uniqueness keys. The queue
supports priority, scheduled execution, bounded retries, exponential backoff,
per-job timeouts, cancellation, crash recovery, deduplication, and visible
terminal failures.

V2 defines four priority classes using River's four priority levels:
interactive, pre-warm, sweep, and background analysis. Unique insertion alone
is insufficient: when interactive work collides with an already available
lower-priority job, the enqueue wrapper promotes the existing job rather than
leaving the user behind the backlog. This promotion uses a River-supported job
update when available; otherwise it is a narrowly scoped, version-pinned SQL
operation covered by queue integration tests. Running and terminal jobs are not
rewritten by priority promotion.

Worker pools are separated by workload:

- Interactive provider searches and missing-entity fetches.
- Routine refresh and enrichment.
- Bulk dump imports and projection rebuilds.
- CPU-heavy fingerprints and ML processing.

Interactive work has reserved capacity. Bulk work cannot consume every provider
token or database connection, and interactive traffic cannot starve bulk work
forever.

## Canonical identity

Identity is the first schema to design and the last place to accept convenient
shortcuts.

### Internal IDs and slugs

Every canonical entity has an immutable, opaque internal ID. Provider IDs and
slugs resolve to that ID but never become it.

Slugs are presentation aliases:

- A current canonical slug is unique within an entity kind.
- Old slugs remain resolvable through a slug-history table.
- Slug changes do not change entity identity.
- Cross-kind slug collisions are valid and never resolved by pretending the
  entities are the same.

Canonical routes address entities by internal ID. Human-readable routes include
the entity kind with the slug, so cross-kind collisions never need a hidden
precedence rule. Search and relation responses return internal IDs for reliable
round trips.

### Entity boundaries

V2 models at least these canonical kinds:

- Movie, show, season, episode, person, character, collection, and company.
- Artist, release group or album, release where needed, recording or track,
  label, and musical work where needed.
- Author, written work, edition, audiobook edition, narrator, and book series.

The public API can continue embedding seasons, episodes, albums, tracks, and
editions. Embedding in a projection does not mean the child lacks its own
identity internally.

Work/edition, recording/release-track, and show/episode boundaries must be
written down per provider before that kind is implemented. Provider-specific
objects are not promoted to canonical entities merely because a provider gives
them an ID.

### External identifier claims

An external ID mapping is a claim with history, not a string column copied onto
an entity forever. A claim records:

- Provider, namespace, and normalized external ID.
- Canonical entity ID and kind.
- Source observation or actor that asserted it.
- Confidence and decision state.
- First and last observed timestamps.
- Whether it is active, rejected, superseded, or disputed.

Active provider IDs are unique within the appropriate provider namespace and
entity boundary. Reassignment, merges, splits, and provider mistakes retain
history. Ambiguous claims go to reconciliation rather than silently joining two
entities.

### Merge, split, and deletion

- A merge chooses a surviving canonical ID and leaves redirects from retired
  IDs and slugs.
- A split creates or restores entities and records which claims and relations
  moved.
- Deletion normally creates a tombstone; it does not erase identity history.
- All three operations are audited, reversible where possible, and emit change
  entries.

## Source ingestion, normalization, and provenance

The ingestion pipeline is:

```text
provider request or dump row
  -> fetch/import observation in Postgres
  -> immutable raw blob in RustFS
  -> versioned normalized source record in Postgres
  -> identity resolution
  -> canonical merge
  -> API and search projections
  -> transactional outbox row
  -> sequenced durable change entry
```

### Blobs and observations are separate

An identical provider response must not consume blob storage twice, but every
fetch still matters operationally.

`source_blobs` records the checksum, object key, compression, media type,
uncompressed and compressed sizes, and integrity state of unique bytes.

`provider_observations` records every request or imported record, including:

- Provider and provider record identity.
- Request shape or dump snapshot.
- HTTP status, selected cache headers, and response timing.
- Fetch time and worker job.
- Normalizer/schema version expected for the payload.
- License or retention class.
- Referenced blob checksum when a body exists.

Repeated unchanged fetches add small observation rows and reuse the existing
blob.

### Normalized source records

Provider adapters produce typed, versioned normalized records between raw bytes
and canonical state. These records preserve source claims without forcing every
leaf into an EAV schema.

A normalized record contains:

- Provider record identity and source observation.
- Entity kind and identity candidates.
- Typed scalar facts, localized values, relations, images, and external-ID
  claims.
- Provider timestamps where available.
- Normalizer version and normalized-record schema version.
- Warnings and partial-failure state.

The normalized shape may use JSONB where source records are naturally nested,
while identity, active external IDs, important relations, and queryable serving
fields use relational tables. JSONB is not an excuse to reproduce raw provider
payloads inside Postgres.

### Canonical merge and field provenance

Merge rules are deterministic and versioned per entity kind. A merge consumes
normalized records plus accepted user overrides and produces canonical state.

Field and relation provenance identifies which normalized record or revision
won. Provenance may be stored by field path or logical scope rather than as one
SQL row per leaf. Important scopes include:

- Titles and overviews by locale.
- Dates and status.
- External IDs.
- Images.
- Cast, crew, members, authors, narrators, and other relations.
- Seasons and episodes.
- Albums and tracks.
- Works and editions.
- Ratings and recommendations.
- Analysis artifacts.

Changing merge rules rebuilds canonical state and projections from retained
normalized records. It does not require provider refetches.

## Postgres serving model

Canonical state and public serving documents are different representations.
Canonical tables preserve identity, constraints, and queryable relationships.
Serving projections are v2-native consumer views. They are designed alongside
the new Heya and web clients rather than inherited from their old decoders.

Core table families are expected to include:

- `entities`, `entity_slugs`, `entity_redirects`, and `entity_tombstones`.
- `external_id_claims` and `external_id_conflicts`.
- `entity_relations` with relation kind, ordering or role, provenance, and
  validity state.
- `provider_observations`, `source_blobs`, and `normalized_records`.
- Kind-specific canonical tables or documents where their access patterns
  justify them.
- `api_documents` and `api_document_provenance`.
- `search_entities` and `search_names`.
- `analysis_artifacts` and lightweight duplicate observations.
- `change_outbox`, `change_log`, audit tables, users, API keys, revisions, and
  moderation state.

### API documents

`api_documents` contains rebuildable, schema-versioned JSONB payloads. It is the
normal Postgres read path for detail endpoints, not a backup copy of raw source
data.

Derived fields such as display titles, flattened search aliases, preferred
artwork, provider status, and freshness are built into projections and versioned
with them. They are not recomputed differently by individual API nodes.

A representative v2 entity document is:

```json
{
  "schema_version": 1,
  "id": "01J...",
  "kind": "show",
  "slug": "breaking-bad-2008",
  "display": {
    "title": "Breaking Bad",
    "year": 2008,
    "image_id": "01K..."
  },
  "external_ids": [
    { "provider": "imdb", "value": "tt0903747" },
    { "provider": "tmdb", "value": "1396" }
  ],
  "data": {},
  "freshness": {},
  "links": {}
}
```

This is illustrative, not a frozen schema. The API specification is designed
after the entity boundaries and normalized records for each vertical slice.

V2 can expose several projections of the same canonical entity:

- Summary documents for search, browse, relations, and cards.
- Detail documents with the normal fields for an entity page.
- Complete snapshot documents for media servers that want one consistent
  metadata bundle.
- Cursor-paginated child collections for very large episode, credit,
  discography, work, or edition sets.
- Explicit `include` expansions when a caller prefers one larger request over
  several child requests.

All useful metadata remains reachable:

- Movie and show APIs expose localized titles and overviews, artwork, ratings,
  credits, recommendations, seasons, and episodes.
- Artist APIs expose biography, links, images, members, top tracks, similar
  artists, releases, albums, and tracks.
- Person APIs expose biography, images, external IDs, and filmography.
- Author and book APIs expose works, editions, audiobooks, contributors,
  narrators, identifiers, and series relationships.
- Specialist analysis and large artifacts are linked through typed endpoints
  rather than embedded as opaque blobs.

Stable internal IDs appear everywhere a canonical entity exists, including
seasons, episodes, people, albums, tracks, works, and editions. External IDs are
typed claim objects instead of being duplicated across envelopes and payloads.

Projection writes use compare/version semantics so an older job cannot replace
a newer result. The canonical update, API projection update, search projection
update or rebuild marker, and unsequenced change-outbox row are committed in the
same transaction. Public cursor assignment happens later through the sequencer
described below.

## Search

Search is implemented in Postgres. There is no Meilisearch service in v2.

`search_entities` stores compact ranking and display fields. `search_names`
stores canonical names, aliases, localized titles, native-script forms,
romanizations, and provider names with locale and source metadata.

The search stack uses:

- A required normalized exact and prefix lookup tier before trigram similarity.
  This handles one- and two-character CJK queries that do not produce useful
  trigrams. Romanizations remain explicit `search_names`; `unaccent` is not
  treated as transliteration.
- `pg_trgm` indexes for typo-tolerant names and titles.
- Normalized and unaccented search forms.
- `tsvector` only where longer text search is useful.
- Exact external-ID lookup before fuzzy matching.
- Kind, year, country, language, and format filters.
- Explicit ranking signals such as exact/prefix match, alias quality,
  popularity, source agreement, and existing-library presence.

Search returns local results immediately and may fan out to upstream providers
when local coverage is insufficient. Provider fanout runs concurrently through
Redis-backed provider rate limits and dedicated interactive River workers.

Ordinary search has a short request budget. Completed provider results may be
included in the response; slower work continues in the background and improves
the local index. Search never waits tens of seconds for every provider.

Postgres also replaces Meilisearch's browse and facet responsibilities.
`search_entities` carries filterable status plus genre, tag, network, and studio
arrays with appropriate GIN indexes. Browse uses the same projection for kind
families, filters, pagination, and result counts. Facet directories and counts
are computed with SQL aggregation; a materialized rollup is introduced only if
normal aggregation later becomes a measured problem.

## Request and refresh behavior

### Existing and fresh

1. Read the versioned Redis API cache.
2. On a miss, read `api_documents` from Postgres.
3. Populate Redis and return the document.

### Existing but stale

1. Return the stale document immediately.
2. Enqueue a deduplicated refresh at interactive priority.
3. Include freshness metadata when useful to the client.

Staleness remains kind- and lifecycle-aware. Active artists, airing shows, new
movies, living people, and settled catalog items do not share one refresh
horizon.

### Missing

1. Resolve the submitted provider ID and enqueue a unique high-priority job.
2. Briefly wait using Redis notification plus a durable River/Postgres state
   check.
3. Return the completed document if it fits the endpoint's request budget.
4. Otherwise return `202 Accepted` with a typed job resource, current partial
   result when useful, retry guidance, and status or event links.

Clients may use `Prefer: wait=<seconds>` within a server-defined maximum to
choose between an immediate job response and a bounded synchronous wait. The
generated Heya client understands both completed entity documents and accepted
job resources; no old status-code behavior constrains this contract.

Search, missing detail, and explicit refresh have different wait budgets.
Imports, full discographies, source dumps, and large fanouts are always
asynchronous.

## Change feed and media-server synchronization

Postgres owns an append-only, cursor-based change log. A projection transaction
writes an unsequenced outbox row atomically with its state change. A single
logical sequencer, made highly available with a Postgres advisory-lock lease,
publishes committed outbox rows into the public log and assigns their cursor
sequence. It may batch and use multiple worker processes, but only the lock
holder assigns public sequence values.

The sequencer selects rows by unsequenced state, never by an outbox-ID
high-water mark: outbox IDs can commit out of allocation order for the same
reason public cursors cannot come directly from a sequence. Assigning public
sequences and making a publish batch visible happen in one transaction.

The visibility invariant is: when sequence `N` is visible to a consumer, every
lower public sequence has already committed and is visible. Writers must not
assign public cursors directly from a Postgres sequence because transactions can
commit out of allocation order and cause consumers to skip late commits.

Each entry records:

- Monotonic sequence ID.
- Entity ID, kind, and current slug.
- Public, internal, moderation, or analysis scope.
- Change type and changed logical fields or child scopes.
- Small before/after summaries where appropriate.
- Actor, API key, provider observation, revision, and River job references.
- Projection schema version and timestamp.

Initial endpoints are expected to include:

```text
GET /api/v2/changes?since=<sequence>&limit=500
GET /api/v2/changes/<sequence>
GET /api/v2/entities/<id>/changes
GET /api/v2/changes/stream?since=<sequence>
```

Sequence IDs are canonical. Timestamp filters may be offered for operator
convenience but are not synchronization cursors.

SSE is the default streaming transport and supports `Last-Event-ID`. Redis
pub/sub wakes connected API nodes, while reconnecting clients always recover
from the Postgres log. Feed retention is explicit. A cursor older than the
retained public log receives a clear full-resync-required response.

Internal provenance-only observations do not have to wake ordinary media-server
consumers. Feed scopes let clients subscribe only to changes that affect their
behavior.

The Heya media server is built to consume this feed as part of the v2 contract.
Long-term retention and full-resync mechanics must be ready before relying on it
as the only synchronization path.

## Authentication, mutation, and moderation

Public metadata reads remain anonymous and read-only. V2 adds separate,
authenticated mutation surfaces for trusted Heya servers and operators.

The initial auth model includes:

- Local users with `user`, `trusted`, and `admin` roles.
- Hashed, revocable API keys whose plaintext is shown only at creation.
- Per-key scopes, quotas, expiry, and last-used metadata.
- Redis-backed request limits and durable usage/audit records where required.
- Operator controls for entity, provider, child, and related-set refreshes.

User metadata corrections are revisions, not direct edits to source data.
Revisions carry old and proposed values, evidence, author, reviewer, status, and
history. Accepted overrides form a distinct provenance layer. Provider refreshes
never silently erase them; conflicts become visible review state. Every
moderation action is reversible and audited.

## Fingerprints and analysis artifacts

Media servers may submit Chromaprint, loudness, waveform, language, subtitle,
OCR, embedding, and ML-tag output through scoped API keys.

Exact artifact identity includes:

- Artifact kind.
- Tool or model and version.
- Normalized payload checksum.
- Source media fingerprint and relevant duration or format metadata.
- Canonical media entity when known.

Exact checksum deduplication and semantic media matching are separate:

- Byte-identical or normalization-identical artifacts reuse one RustFS blob.
- Chromaprint matching uses compatible tool versions, fingerprint similarity,
  and duration tolerance rather than checksum equality alone.
- Duplicate submissions may add lightweight observations, confidence, source
  server, and timestamps without copying the heavy payload.
- New model versions may coexist with older versions under explicit retention
  rules.

Successful submissions return idempotent 2xx results such as `accepted`,
`already_matched`, `duplicate_observed`, or `superseded`.

Artifacts without a known tool version or required source-media properties are
not accepted as canonical analysis. The submission boundary validates and
records this provenance from the beginning.

## Machine-generated translations

Translation gap filling is a derived-data pipeline, not part of the initial
canonical merge.

A generated translation records:

- Source and target language.
- Source text checksum.
- Translation provider or local model and exact version.
- Generation time, confidence where available, and review state.
- The field and entity to which it applies.

Generated translations remain distinguishable from upstream and human-edited
translations. When source text changes, its generated translations become stale
and are queued for regeneration. Coverage is driven by configured or requested
languages and product usage, not merely by whichever languages happen to exist
on the top-level overview.

## Community segments

Intro, recap, credits, preview, commercial, outro, and other skip segments use
the same identity, provenance, artifact, auth, and change-feed foundations.

Source candidates remain separate and include media fingerprint, duration,
time range, label, provider or submitter, and confidence. Matching candidates
may later produce a consensus suggestion, while per-server overrides stay local.
The v2 segment API is designed around these source candidates and canonical
media identities. Community consensus and voting do not block the initial
metadata platform.

## Storage discipline and the 1 TB budget

One terabyte is a useful starting allocation only if retention is designed in,
not added after the bucket is full.

Rules:

- Anything rebuildable is a projection.
- Anything large is compressed and content-addressed.
- Anything queried frequently has an intentional Postgres representation.
- A fetch observation is retained separately from its deduplicated bytes.
- Caches can always be deleted.
- Dumps, ML models, intermediate artifacts, image variants, and detailed diffs
  have explicit retention classes.
- No worker may create an unbounded new artifact namespace without metrics and a
  retention policy.

RustFS usage is measured by prefix, provider, artifact kind, and retention
class. Alerts fire on growth rate and at high-water marks rather than only when
the bucket is nearly full. Garbage collection is mark-and-sweep from live
Postgres references with a quarantine period; an object is never deleted merely
because one referencing row disappeared.

Images are expected to be a primary pressure source: profile artwork for large
cast and crew catalogs multiplies much faster than title count. Full source
dumps and versioned ML output are the other likely large consumers; ordinary
compressed JSON responses are comparatively small.

Image storage is metered by artwork class from launch. Cast and crew profiles
retain the serving WebP but not the upstream original, and derived variants are
bounded. Dump retention is decided per provider and license; keeping every
historical dump is not the default.

## Reliability and operations

- Postgres backups and point-in-time recovery are tested through restores.
- RustFS data and metadata durability are monitored independently of Postgres
  blob references.
- Redis persistence may aid recovery but is never relied upon for canonical
  correctness.
- Every provider has an explicit timeout, retry policy, concurrency limit,
  shared quota, user agent, and circuit-breaker behavior.
- Worker and API database pools have separate limits.
- Jobs expose queue time, run time, attempts, next retry, failure class, and
  owning worker.
- Metrics cover cache hit rate, provider latency/error rate, projection lag,
  queue depth by priority, blob growth, dedupe ratio, and change-feed lag.
- Structured logs correlate request, job, observation, entity, projection, and
  change IDs.
- The enrichment inspector can display raw observations, normalized records,
  merge winners, projection versions, provider failures, and refresh controls.

## Clean-slate implementation and cutover

V2 neither shadows the old internals nor imports their database by default. It
is built as a complete service with new clients, then selected as a unit when its
metadata coverage is ready.

### 1. Build the metadata coverage catalog

Inventory the information the product should continue to offer:

- Movies, shows, seasons, episodes, anime mappings, specials, segments,
  collections, recommendations, and credits.
- People, characters, aliases, biographies, filmographies, and artwork.
- Artists, members, relationships, releases, albums, tracks, recordings,
  credits, similar artists, and audio fingerprints.
- Authors, written works, editions, audiobooks, contributors, narrators, and
  series.
- Localized titles and descriptions, ratings, images, videos, dates, status,
  identifiers, provider errors, and provenance across every kind.
- Search, browse, facets, external-ID resolution, change synchronization, and
  operator inspection.

Old payloads, UI screens, and provider adapters can help discover fields and
edge cases, but do not become golden response fixtures. Tests assert normalized
facts, identity decisions, merge invariants, and metadata presence rather than
old JSON equivalence.

The catalog is executable, not prose. Each entry names the field or
relationship, the providers that can supply it, and at least one reference
entity per kind for which v2 must surface that fact with expected provenance.
Reference entities include hard cases such as anime with episode-numbering
offsets, artists with very large discographies, ambiguous identities, partial
provider failures, missing artwork, and malformed upstream responses.

Coverage checks assert facts and provenance through v2's own API shapes; they
never compare old document structure. "At least the same useful metadata" means
every catalog entry known to be obtainable from the previous source set passes,
while newly supported metadata and providers add new entries.

### 2. Design each domain and its API together

Before implementing an entity kind, write its identity boundaries, normalized
source schema, canonical merge rules, projection shapes, search representation,
and API resources. Publish an OpenAPI contract and generate the Heya client from
it.

The API design includes pagination, expansion, job resources, errors,
freshness, provenance, and change-feed behavior from the beginning. It does not
add compatibility aliases or duplicate fields for old clients.

### 3. Establish all four platform roles

Bring up Postgres, RustFS, Redis, and separate API and worker processes together.
Install required Postgres extensions and River schema. Establish schema
versioning, secrets, backups, health checks, metrics, and local development
parity before provider implementation begins.

### 4. Implement complete vertical slices

Each entity kind passes through the complete pipeline: provider observation,
RustFS blob, normalization, identity resolution, canonical merge, API
projection, Postgres search projection, Redis caching, River refresh, and
change feed.

A practical order is:

1. Movies as the first complete serving slice.
2. Shows, seasons, episodes, anime mapping, people, and segments.
3. Artists, release groups, releases, recordings, tracks, and fingerprints.
4. Authors, works, editions, audiobooks, and narrators.

The order can change with product needs. A slice is complete only when its
coverage catalog, provenance, failure behavior, API, search, admin inspection,
and retention rules are implemented.

### 5. Build the clients against v2

The Heya media server and web frontend use generated v2 types and are free to
change their own storage and workflows. They should exercise job responses,
change cursors, paginated child collections, explicit expansions, opaque entity
and image IDs, and the new auth model directly rather than through adapters that
imitate v1.

### 6. Populate and cut over

Populate v2 from official dumps, provider APIs, and normal on-demand requests.
The old Mongo documents, Meilisearch index, image keys, and job ledger are not
required seeds.

Run the complete v2 service and updated clients in a separate environment until
the coverage catalog and operational readiness checks pass. Cut over the Heya
and web releases together.

Rollback during the initial confidence window means redeploying the previous
client releases against the intact old service, not repointing v2 clients at an
API they do not understand. The previous client releases remain deployable, and
client-side storage changes made for v2 stay additive or reversible, until the
window closes. Old and new entity behavior are never mixed inside the v2
architecture.

## Decisions still requiring focused design

The system choices above are settled. These domain decisions need short,
kind-specific design documents before implementation:

- Canonical boundaries and merge/split rules for every entity kind.
- Which relations require their own canonical entities versus provider-scoped
  normalized records.
- Exact normalized-record schemas and field-provenance granularity.
- Summary, detail, snapshot, expansion, and pagination boundaries in the v2 API.
- Retention periods per provider license, dump type, image class, and analysis
  model.
- Which authenticated submissions are accepted immediately and which require
  moderation.
- Full-resync mechanics and public change-feed retention duration.

These are not reasons to revisit Postgres, RustFS, Redis, River, or trigram
search. They are the domain work required to use that platform correctly.

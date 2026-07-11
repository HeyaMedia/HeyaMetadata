# Provider blueprint

Every upstream adapter implements the shared collector contract in
`internal/providers`. A capability declares accepted identifiers, provided
scopes, raw-blob retention, and exact-response reuse policy. Provider-specific
code builds requests and normalizes responses; caching, locking, evidence
storage, and request-scoped credentials stay shared infrastructure.

## Exact response reuse

The request fingerprint is SHA-256 over the provider name and a credential-free
request key. The request key must include every input that can change the
response: endpoint, provider ID, locale, region, query, pagination, and appended
scopes. It must never contain an API key or token.

Resolution order is:

1. Read the small Redis pointer and optional hot response body.
2. On a Redis miss, find a still-reusable observation in Postgres.
3. Load and verify its content-addressed body from S3.
4. On an S3 `NoSuchKey`, invalidate the pointer, mark the blob missing, and
   fetch upstream.
5. Before fetching, acquire a Redis lock for the full request fingerprint and
   double-check shared storage after acquiring it.
6. Persist one immutable observation, then publish the Redis pointer/body.

Redis is an accelerator and coordination layer, not the source of truth. A
successful TMDB response is reusable for 48 hours; a 404 is reusable for one
hour. Authentication, authorization, throttling, and server failures are not
reused. Bodies up to 1 MiB stay hot in Redis for up to one hour. Raw TMDB
evidence remains under the independent 48-hour S3 lifecycle prefix.

`reusable_until` answers “may this response avoid an upstream request?” while
`source_blobs.expires_at` answers “how long may the evidence bytes exist?” Keep
those concepts separate even when a provider initially gives them equal values.

## Request-scoped provider credentials

API callers may send `X-Heya-TMDB-API-Key` on resolution and refresh requests.
The API stores the credential in Redis for at most two hours and puts only a
random, opaque reference in the River job. Workers resolve the reference when
they run and delete it after success or a terminal not-found response.
Plaintext provider credentials are never written to River/Postgres, observation
headers, request fingerprints, blob metadata, or application logs.

TMDB uses a caller key as its `api_key` query parameter. If no caller key is
present, the configured server read token is used as a Bearer token. Warm cache
hits do not require either credential and consume no provider quota.

For another provider:

1. Add its explicit API request header and place the value in
   `providercredentials.Credentials.APIKeys` under the normalized provider name.
2. Pass only the opaque credential reference through the job args. Do not add
   it to River uniqueness; jobs remain unique by logical entity/request.
3. Read the provider key only while constructing the real upstream request.
4. Ensure transport errors cannot print URLs containing query-string secrets.

Redis credential handoff is intentionally temporary. A job that cannot resolve
an explicitly supplied credential is cancelled instead of silently charging a
different caller or persisting the secret elsewhere.

## River history retention

River's built-in leader-elected job cleaner removes completed jobs 24 hours
after `finalized_at`. This policy is explicit in the shared client config, so it
applies to ingestion, retention, and future provider jobs without scheduling a
cleanup job that would itself create queue history. River v0.40 runs the cleaner
more frequently than hourly (every 30 seconds by default), but only jobs beyond
the 24-hour horizon qualify. Domain ledgers and provider observations remain in
their own tables after the queue row is removed.

## Demand-aware refresh priority

River priority bands are shared across providers:

- `1` — interactive resolution, explicit refresh, and CLI requests.
- `2` — stale-on-read refreshes where an existing document can still be served.
- `4` — scheduled refresh and storage-maintenance work.

Movie ingestion remains unique by TMDB ID. If an interactive request collides
with an already queued background refresh, the existing job is promoted in
place and may receive the caller's opaque credential reference. A duplicate job
is not created, and a running job is never mutated.

Successful entity detail reads increment a Redis-buffered access counter. The
hourly `adaptive_refresh_scheduler_v1` job flushes those counters into
`entity_access_stats`, applies exponential score decay, recalculates each
provider's `next_eligible_at`, and enqueues due refreshes at priority 4. Current
cadence bands are:

- fetched in the last 2 days or very high decayed demand: every 2 days;
- fetched in the last 14 days or sustained demand: every 7 days;
- fetched in the last 60 days: every 14 days;
- colder or never fetched: every 30 days.

Search result impressions do not count as accesses; a detail fetch or resolved
entity does. Future upstream search collectors should enqueue missing entities
at interactive priority but only record demand when a user actually fetches the
entity. Redis counter claims are restored if the Postgres flush fails.

## OMDb refinement

OMDb is the first supplemental collector. TMDB runs from a `tmdb.movie`
identifier, its normalized IMDb claim is fed back to the planner, and OMDb then
runs from `imdb.title`. Replanning skips completed providers but starts with the
full desired scope set, so a supplemental source can add overlapping ratings or
descriptions instead of being suppressed because TMDB supplied that scope.

OMDb accepts `X-Heya-OMDB-API-Key` through the same transient credential map and
falls back to `HEYA_METADATA_OMDB_API_KEY`. Exact successful responses are
reusable for 24 hours, hot bodies for one hour, and raw evidence for 48 hours.
The shorter reuse window reflects changing IMDb, Rotten Tomatoes, and
Metacritic ratings.

Some providers encode application failure inside an HTTP 200 body. Payloads can
therefore override status-derived reuse after provider-specific classification
but before shared persistence. OMDb “movie not found” responses receive a
one-hour negative TTL; invalid-key, quota, malformed, and other logical failures
are recorded but never reused. This prevents one caller's bad credential from
poisoning the credential-independent shared cache.

## TVDB refinement

TVDB is unlocked from the IMDb claim through its remote-ID search. The search
and `/movies/{id}/extended` response are separate cached observations; the
search observation is supporting evidence for the normalized TVDB movie. Empty
remote-ID searches use a one-hour negative TTL.

TVDB authentication is prepared only when a network request is actually
required, so a provider-cache hit never performs `/login`. The official token
is valid for one month. Server-key tokens are retained in Redis for 25 days and
keyed by a hash of the API key; request-scoped `X-Heya-TVDB-API-Key` tokens stay
inside the ingestion job and are not shared. A 401 invalidates a cached token.

The movie normalizer preserves TVDB aliases/translations, genres/tags, release
and certification evidence, companies, identity claims, people credits, and
typed artwork. TVDB `score` remains a provider popularity signal and is never
presented as a rating. Identity candidates from every successful supplemental
record participate in conflict detection and are attached as durable claims.

## Fanart.tv refinement

Fanart.tv v3.2 is the dedicated supplemental movie-artwork collector. It runs
from the canonical `tmdb.movie` identifier and preserves posters, backgrounds,
logos, banners, clear art, thumbnails, and disc art with provider image IDs,
languages, dimensions, likes, and source provenance. The provider's `00`
language sentinel becomes an unspecified language, and legacy HTTP asset URLs
are upgraded to HTTPS.

The configured `HEYA_METADATA_FANART_API_KEY` is the application/project key.
Callers may additionally send their personal key as
`X-Heya-Fanart-API-Key`; it becomes Fanart.tv's `client_key` and uses the same
transient Redis credential handoff as the other providers. Either key is enough
for a direct request. Neither appears in the request fingerprint, observation
metadata, River job, or logs. Successful responses are reusable for 24 hours,
empty successful envelopes for one hour, and raw evidence expires after 48
hours.

Fanart.tv precedes TVDB in follow-up planning so its artwork scope is collected
while TVDB remains available for credits and its other scopes. The combiner
does not choose one provider globally: all artwork candidates receive opaque
IDs and retain provider provenance for later ranking.

## MusicBrainz source collection

MusicBrainz starts the music-source phase without introducing canonical music
merge rules. Its collector keeps `artist`, `release_group`, `release`, and
`recording` as separate provider namespaces, matching MusicBrainz's own
identity boundaries. Known MBIDs support rich lookups; the source client also
supports paged Lucene search and correctly paged release-group browsing for an
artist. Search hits remain candidates and never become identity claims until a
specific MBID is collected.

The public MusicBrainz service requires a meaningful User-Agent and averages at
most one request per second per source IP. Every client for the same base URL
therefore shares an in-process request gate, applied only immediately before a
real HTTP request so cache hits do not wait. Mirrors can configure a different
rate with `HEYA_METADATA_MUSICBRAINZ_REQUESTS_PER_SECOND`. Exact lookups are
reusable for 12 hours, volatile search/browse pages for six hours, missing
records for one hour, and raw evidence expires after 48 hours. Malformed or
identity-mismatched HTTP 200 responses are recorded but never reused.

This phase deliberately archives typed provider source evidence without a
canonical artist/album/edition/track projection. The recording versus release
track and release-group versus release boundaries must be written down before
those entity kinds enter identity resolution and merge.

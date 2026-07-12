# Heya Metadata v2 handoff

## Start here

This repository is the clean-slate replacement for the metadata service in
`~/Private/HeyaMedia`.

Read [HeyaMetadataV2.md](./HeyaMetadataV2.md) before making architectural
decisions. It was reviewed iteratively by Codex and Claude/Fable; the final
clean-slate revision was approved by both.

## User intent

- This is an entirely new implementation.
- The current HeyaMedia code, database, API, payloads, slugs, image keys, and
  client behavior impose no compatibility requirements.
- The Heya media server and web frontend can change together with this service;
  the user is their only operator.
- Continuity means retaining at least the same useful metadata and provider
  coverage, with provenance. It does not mean retaining the same JSON paths or
  endpoint behavior.
- Additional metadata sources are welcome.

## Settled architecture

- Go service and CLI.
- Postgres owns canonical and operational state.
- River is the durable job system.
- Redis is included from the first iteration for shared cache, locks, quotas,
  rate limiting, and short-lived notifications.
- RustFS at `s3.karbowiak.dk` is the initial S3-compatible blob store, with
  roughly 1 TB available.
- Search, browse, and facets use Postgres `pg_trgm`, normalized exact/prefix
  lookup, full-text search where useful, and Postgres indexes. No Meilisearch.
- Raw provider observations are immutable, compressed, and content-addressed.
- Canonical identity is independent of provider IDs and slugs.
- API documents are rebuildable projections, not source truth.
- The API, generated clients, opaque entity IDs, opaque image IDs, `202` job
  resources, pagination, and expansions are all v2-native.
- The durable change feed uses a transactional outbox and a single logical
  sequencer; writers never allocate public cursors directly.
- Metadata continuity is verified through an executable semantic coverage
  catalog: facts and provenance are tested through v2's API shapes, never by
  comparing old documents.

## Current filesystem state

The repository is initialized on `main`. The initial CLI/server scaffold is a
passing baseline, the first domain-design milestone is present, and the core
platform foundation is implemented and locally validated.

Created files:

```text
.env.example
.gitignore
Makefile
README.md
go.mod
cmd/heya-metadata/main.go
cmd/heya-metadata/cmd/root.go
cmd/heya-metadata/cmd/serve.go
cmd/heya-metadata/cmd/version.go
cmd/heya-metadata/cmd/openapi.go
internal/buildinfo/buildinfo.go
internal/config/config.go
internal/server/server.go
internal/server/health.go
internal/server/server_test.go
internal/ui/banner.go
internal/ui/format.go
internal/ui/output.go
internal/ui/theme.go
coverage/catalog.go
coverage/catalog_test.go
coverage/movie.json
coverage/README.md
docs/domains/movie.md
AGENTS.md
.air.toml
.air.worker.toml
compose.yaml
mprocs.yaml
internal/blobstore/s3.go
internal/jobs/client.go
internal/jobs/smoke.go
internal/jobs/movie.go
internal/jobs/retention.go
internal/migrations/migrations.go
internal/migrations/sql/0001_platform.sql
internal/migrations/sql/0002_canonical_pipeline.sql
internal/providers/contracts.go
internal/providers/tmdb/
internal/ingest/observations.go
internal/mixer/planner.go
internal/domains/movie/
internal/movies/
internal/server/movies.go
internal/platform/runtime.go
internal/devproxy/proxy.go
web/package.json
web/nuxt.config.ts
web/app/app.vue
tools/dev/check-ports.sh
tools/dev/prune-go-cache.sh
```

The current command surface is:

```text
heya-metadata
heya-metadata serve
heya-metadata version
heya-metadata openapi-spec
heya-metadata migrate up
heya-metadata migrate status
heya-metadata worker
heya-metadata smoke
heya-metadata movie ingest --tmdb 603
heya-metadata retention sweep
```

`heya-metadata dev-proxy` is a hidden development command used by `make dev`.
The development topology is:

```text
:3030 stable dev proxy -> /api* -> :3031 Go API under Air
                       -> /*    -> :3032 Nuxt/Vite

River worker under Air -> shared .dev/air/heya-metadata binary
```

`make dev` starts Docker Compose first. Postgres 18 is bound to `127.0.0.1:5441`
and Redis 8 to `127.0.0.1:6380`; application and River migrations run before
mprocs starts. The existing `heyamedia` bucket is accessed through the RustFS
API at `https://s3-api.karbowiak.dk`; v2 metadata is isolated under `data/` and
the existing `images/` namespace remains reusable. No local S3 stand-in is
started. Put its credentials in the gitignored `.env.local` file using
`.env.example` as the template.

Air is an external development prerequisite (`brew install go-air`) rather than a
Go tool dependency, keeping its large development-only dependency graph out of
the application module. It overwrites one binary path under `.dev/air`. Go's
project-local build cache lives under `.cache/go-build` and is cleared after a
successful Air build when it exceeds 512 MiB by default. The module download
cache is separate under `.cache/go-mod`.

The scaffold currently assumes:

- Module path: `github.com/HeyaMedia/HeyaMetadata`.
- Binary name: `heya-metadata`.
- Default port: `3030`.
- Cobra for commands.
- Lip Gloss v2 for styled terminal output.
- Huma v2 on `net/http` for the API and OpenAPI docs.
- Standard-library `slog` for logging.

Those names and defaults were retained deliberately when the scaffold was
validated.

## Validation status

- `go mod tidy` completed and `go.sum` exists.
- `go fmt ./...`, `go test ./...`, and `go build ./...` pass with Go 1.26.5.
- The movie coverage catalog is embedded and structurally validated during
  `go test ./...`.
- The legacy movie path was inspected for semantic coverage. No legacy code or
  API shape was copied into v2.
- The API docs use Scalar rather than Stoplight Elements.
- `make dev` supervises the proxy, Air backend, and basic Nuxt 4 frontend with
  mprocs. It also supervises a separate River worker which restarts from the
  same binary that API Air rebuilds. The frontend is intentionally a placeholder.
- Docker Compose starts healthy PostgreSQL 18 and Redis 8 services with
  persistent named volumes. PostgreSQL 18's volume is correctly rooted at
  `/var/lib/postgresql`.
- Embedded application migrations enable `pg_trgm` and `unaccent`, establish
  immutable content-addressed blobs and provider observations, and include a
  platform smoke ledger. River owns and applies its own schema migrations.
- Running migrations twice is verified: the second run applies zero application
  and zero River migrations.
- Readiness probes Postgres, Redis, and S3 concurrently. Liveness remains
  independent of dependency health.
- The `platform_smoke_v1` River job exercises S3 blob put/get, Redis set/get,
  and transactional Postgres recording with retry-safe immutable observations.
- The existing Heya S3 credentials are present in the gitignored `.env.local`.
  A real `platform_smoke_v1` run passed through River, RustFS under
  `heyamedia/data/blobs/...`, Redis, and transactional Postgres recording.
- The first movie vertical slice is implemented. Real TMDB ingestions for The
  Matrix (`603`) and Spirited Away (`129`) passed through River, separate raw
  observations, normalized movie records, opaque identity claims, deterministic
  combination, provenance, detail/summary projections, Postgres search, Redis,
  and the gap-free public change feed.
- Shared collector capabilities declare accepted identifiers, provided scopes,
  raw-blob retention, and exact-response reuse. The mixer can re-plan when a
  collector discovers IDs that unlock another provider. Domain combiners union
  provider evidence while applying explicit precedence only to scalar winners.
- TMDB now exercises the reusable provider-cache blueprint: Redis pointer and
  one-hour hot body, durable Postgres/S3 fallback, checksum verification,
  per-request distributed locking, 48-hour success reuse, and one-hour negative
  reuse. Live jobs 23 and 24 added no observations after job 22 fetched the movie
  and collection; job 24 was run after evicting the Redis pointer/body keys.
- Resolution and refresh endpoints accept `X-Heya-TMDB-API-Key`. Plaintext keys
  live only behind a two-hour Redis credential reference; River persists the
  opaque reference. A live refresh job completed from cache with a deliberately
  invalid caller key, and its River args contained no plaintext credential.
- River's built-in leader-elected cleaner is explicitly configured to retain
  completed jobs for 24 hours. In River v0.40 it checks every 30 seconds, so no
  self-generating hourly cleanup job is needed; domain ledgers remain durable.
- River priority bands are now interactive `1`, stale-on-read `2`, and scheduled
  maintenance `4`. Unique queued movie jobs are promoted in place when a user
  request overtakes background work, including safe attachment of an opaque
  caller-credential reference.
- Detail demand is buffered in Redis and flushed hourly into durable
  `entity_access_stats`. The adaptive refresh scheduler decays demand and uses
  2/7/14/30-day cadence bands, with cold or never-read entities settling at one
  month. A live detail read flushed successfully and moved the Matrix refresh
  cadence to two days; a real integration test proved priority promotion.
- OMDb is now the second movie collector. The planner replans after TMDB
  discovers an IMDb title ID, records a separate OMDb observation/normalized
  record, and combines plot/runtime fallback plus independent IMDb, Rotten
  Tomatoes, and Metacritic scales and provenance. OMDb success reuse is 24h,
  not-found reuse is 1h, and application/authentication failures are not reused.
- `X-Heya-OMDB-API-Key` uses the same transient Redis credential handoff. The
  existing old-server OMDb key is present in the gitignored `.env.local`.
- Live Matrix ingestion produced TMDB 8.25/10, IMDb 8.7/10 with votes, Rotten
  Tomatoes 83/100, and Metacritic 73/100. A repeat added zero observations. A
  deliberately invalid caller key on Spirited Away produced a non-reusable 401;
  the following server-key refresh fetched 200 and cleared the provider failure.
- TVDB is implemented and integrated for movies. IMDb `tt0133093` resolved to
  TVDB movie `169`; remote search and extended detail are separate observations.
  The Matrix projection now includes TVDB freshness, genres, companies, credits,
  artwork, and a durable `tvdb.movie:169` external-ID claim. TVDB login is lazy,
  server token reuse is 25 days, and request-scoped keys are supported through
  `X-Heya-TVDB-API-Key` without sharing their bearer token.
- Fanart.tv v3.2 is implemented and integrated for movie artwork. It accepts
  the configured project key plus an optional transient personal key through
  `X-Heya-Fanart-API-Key`, and neither secret affects the shared request
  identity. A live Matrix ingestion recorded one reusable Fanart observation,
  normalized 116 typed artwork candidates, and projected Fanart freshness and
  provenance; an immediate repeat added zero observations.
- MusicBrainz source collection supports validated artist, release-group,
  release, and recording MBID lookups plus paged search and artist
  release-group browsing. Public-service requests share a one-per-second gate,
  carry the required meaningful User-Agent, and use 12-hour lookup / six-hour
  discovery reuse. All lookup include combinations were checked against the
  live WS/2 JSON API. Canonical music boundaries and merge precedence remain
  intentionally undecided.
- Apple, Deezer, Discogs, and Last.fm source collectors are implemented. Apple
  uses the official free iTunes Search/Lookup API, preserving storefront
  identity without Apple Developer Program credentials and respecting its
  documented conservative request limit; Deezer
  classifies HTTP-200 error envelopes; Discogs separates artist/release/master/
  label and keeps tokens in headers; Last.fm is keyed from MusicBrainz IDs and
  classifies its JSON error codes. Search and catalog pagination inputs are
  explicit in every cache fingerprint. Existing Discogs and Last.fm keys are
  available only in the ignored local environment. Representative live lookup
  and search calls for all four providers returned HTTP 200.
- AniDB and TVMaze source collectors are implemented. AniDB is pinned to its
  official plaintext port-9001 XML API, enforces the registered-client shape,
  spaces requests by two seconds, and reuses exact anime data for 24 hours as
  required by its anti-flood guidance. TVMaze resolves IMDb/TVDB/TVRage IDs and
  follows with rich show embeds, plus people and search surfaces. Live AniDB
  anime `1`, TVMaze IMDb lookup, and a 146 KB embedded show response returned
  HTTP 200. XML observations now use `.xml.gz` content keys.
- Wikidata and Open Opus source collectors are implemented. Wikidata separates
  stable EntityData lookups from `wbsearchentities`, sends the required contact
  User-Agent, and serializes calls at one per second. Open Opus separates
  composers and works, collecting complete composer catalogs plus work detail
  and discovery. Live Q42 EntityData was 309 KB, Beethoven's Open Opus catalog
  was 52 KB, and representative search/detail calls returned HTTP 200.
- All nine pre-merge source collectors are registered behind the generic
  `source_collect_v1` River job and `heya-metadata provider collect` CLI. The
  durable run ledger records observation IDs plus fetched/reused counts, job
  uniqueness ignores transient credentials, and `--api-key` uses the opaque
  Redis handoff. Live River jobs archived 11 HTTP-200 observations across
  MusicBrainz, Apple, Deezer, Discogs, Last.fm, TVMaze, Wikidata, and Open Opus;
  an immediate MusicBrainz repeat reused its original observation, while a new
  Open Opus work correctly reported `recorded=1 reused=0`. AniDB was not fetched
  a second time because its official anti-flood policy forbids repeated daily
  requests for the same anime.
- Raw provider bytes use prefix-scoped RustFS lifecycle expiry:
  `data/ephemeral/24h/` expires after one day and `data/ephemeral/48h/` after
  two. TMDB uses the 48-hour tier. No rule matches `images/` or permanent data.
  The live lifecycle export is committed under `ops/rustfs/`.
- Hourly River retention reconciliation waits a 24-hour lifecycle grace period,
  then performs an idempotent fallback delete and marks the Postgres blob row.
  Observation metadata and normalized records remain. The manual equivalent is
  `heya-metadata retention sweep`.
- Public documents use opaque IDs for movie art, profiles, studio logos,
  collection members, and recommendation posters; upstream image URLs remain
  internal evidence only.
- In restricted Codex environments, set `GOPATH` and `GOCACHE` under `/tmp` so
  Go does not try to write outside the workspace.

## Suggested next turn

The first canonical music slice is now implemented. `docs/music-domain.md` and
`coverage/music.json` define the identity boundaries. A MusicBrainz artist MBID
acts as the initial spine and explicit provider relationships unlock Apple,
Deezer, Discogs, Last.fm, and Wikidata. All six sources normalize into
provider-scoped artist evidence, and the deterministic combiner emits one
canonical artist detail/summary/search projection with provenance and opaque
image candidate IDs. Discogs aliases and Last.fm name-only similar artists are
deliberately not identity links.

The durable entry points are `artist_ingest_v1`, `heya-metadata artist ingest
--musicbrainz <mbid>`, and the generic `/api/v2/resolutions` endpoint with
`kind=artist`. Artist reads, refreshes, mixed movie/artist search, job status,
adaptive refresh state, cache invalidation, and change outbox sequencing are
wired. Request-scoped Apple, Discogs, and Last.fm credential headers are
documented in OpenAPI and handed to workers through transient Redis references.

Live verification on 2026-07-11 ingested The Beatles and Radiohead. Both
resolved all six providers without partial failure. The Beatles projection had
9 strong external IDs and 99 internal image candidates.

Lazy image materialization is now implemented through `image_materialize_v1`
and `GET /api/v2/images/{id}`. It validates HTTPS and provider-specific hosts,
follows only allowed redirects, caps originals at 25 MiB, verifies supported
image MIME signatures, and stores content-addressed originals below
`data/images/original/`. A live Beatles Discogs candidate was materialized and
served as an immutable JPEG through its opaque image ID.

The canonical release-group slice is also live. MusicBrainz owns the initial
work-level identity and exposes its distinct release/edition IDs. Explicit
MusicBrainz relations unlock Wikidata and a Discogs master; Wikidata authority
claims then unlock Apple, Deezer, and Spotify album IDs without title matching.
Apple and Deezer catalog albums remain provider editions beneath the canonical
work. Discogs representative tracks remain provider-scoped and are not treated
as MusicBrainz recordings.

`release_group_ingest_v1`, `heya-metadata release-group ingest`, generic API
resolution/refresh, mixed search, adaptive refresh, provenance, and change
sequencing are wired. A live Abbey Road run combined five fetched providers
into seven strong IDs, 28 editions, 52 provider-scoped tracks, and nine image
candidates without partial failure. Last.fm's contract was corrected: its
album lookup consumes a MusicBrainz release MBID, not a release-group MBID.

Smart upstream discovery is now separate from canonical search.
`GET /api/v2/search` remains local-only, accepts a canonical `kind` filter, and
uses short Redis caching; live warm artist search was below 1 ms at the API.
`POST /api/v2/discoveries` creates or reuses `discovery_search_v1`, waits only a
1.2-second default budget, and otherwise returns `202` while the interactive
River job continues. Normalized identical requests share a six-hour result.

Artist discovery currently searches MusicBrainz and ranks candidates using
provider score, normalized primary/alias match, country, area, artist type,
birth/founding and end dates, and verified release-group title/year hints.
Every result includes weighted evidence, ambiguity recommendation, existing
canonical entity ID, and the exact resolution payload. Live `ano` hints chose
the Japanese artist at 0.99 over the German rapper; `Balloon` stayed ambiguous
without hints and became a strong match using Monstersound/Pussylovers; Haku
release hints selected the populated `ハク。` entry over its duplicate shell.

The consuming-server contract is captured in
`docs/client-resolution-flow.md`: local search, upstream discovery (including
discovery polling), explicit candidate resolution (including ingestion-job
polling), and the final canonical entity read. The Heya entity UUID is the
durable identity; provider IDs remain resolution inputs.

Movie and Release Group now reuse that smart-discovery contract. Movie search
uses TMDB with request-scoped credentials carried to River by opaque Redis
reference, and ranks title/original title, year/date, language, country, and
alternate-title evidence without hard-filtering on possibly imprecise years.
Release Group search uses MusicBrainz and ranks title/alias, date/year, type,
credited artist names/MBIDs, and track hints verified through recording release
relationships. Both return existing canonical IDs and exact resolution bodies.
The CLI exposes `discover movie` and `discover release-group`.

Live end-to-end checks passed. Dune (2021) was absent locally, discovered as
TMDB 438631, resolved to Heya entity `247775bd-484e-4e16-82da-335ee91af42f`,
and combined TMDB, OMDb, TVDB, and Fanart. Ado's 残夢 (2024) was discovered at
0.99 using artist and verified track evidence, then resolved through the async
job boundary to `85c83fd2-078c-404e-b482-deb397076656` with MusicBrainz,
Discogs, and Wikidata evidence. The normal 15-second negative canonical-search
cache expired and the new movie then appeared on the local fast path.

TV and Anime are now separate canonical kinds and API families, documented in
`docs/tv-anime-domain.md`. TVMaze-backed TV discovery and canonical ingestion
retain seasons, episodes, networks, and explicit remote IDs. TV ingestion now
uses those explicit IDs to mix TVDB extended-series and TMDB TV/season evidence;
live Game of Thrones combines all three providers into exactly 73 conventional
episodes, each retaining all three numbering schemes. Season-zero clip catalogs
are excluded and provider artwork candidates are bounded. AniDB-backed Anime
discovery uses the official daily title dump, registered client parameters, the
old `heya-media/1.0 anidb-titles-sync` user agent, and rate-limited detail
enrichment. Anime now adds the Fribb anime-lists identity bridge and only the
explicitly mapped TVDB season, including split-cour offsets. Ambiguous AniDB
related IDs are superseded rather than exposed as canonical claims. Live Cowboy
Bebop combines AniDB, anime-lists, and TVDB while retaining special/credit/
trailer/parody schemes. Shared primitives exist below the domain boundary;
there is no `is_anime` flag or shared canonical identity.

Music release presentation dedup now uses `internal/textmatch`: Kagome IPA
compound readings, kana romanization, and Unidecode comparison keys bridge
native/romanized titles while preserving originals. Cross-provider equivalent
editions retain all IDs in `sources`; remixes/live/remasters remain distinct.

The first canonical issued-release slice is live. `release` is distinct from
`release_group`; ordered media and release-track placements retain printed and
provider numbering, while referenced MusicBrainz recordings materialize once
as reusable canonical `recording` entities. The CLI command is
`heya-metadata release ingest --musicbrainz <release-mbid>`. Resolution and
dedicated `/api/v2/releases/{id}` and `/api/v2/recordings/{id}` reads work, and
recordings participate in the fast local search index. Live Ado 残夢 release
`044d87f2-9fda-475a-b041-47df9443a3f5` produced one medium, 16 release tracks,
16 canonical recordings, and retained ISRC evidence. ISRC claims stay proposed
and collisions open conflicts rather than auto-merging recordings.

Issued releases now use a reusable verified supplemental mixer for free iTunes
Search, Deezer, and Discogs. iTunes searches album candidates and requires
artist/title/year plus strong track-layout agreement; Deezer uses its UPC album
lookup; Discogs searches releases by barcode with its request-scoped/configured
token and verifies the fetched release. Barcode paths require exact normalized
barcode plus complete track layout and compatible year/title. Tracks match by
ISRC first, then verified disc/sequence/title/duration. Live Abbey Road
release `31765b9f-e969-4257-855f-c7ea1f657b2a` combined MusicBrainz release
`31765b9f-e969-4257-855f-c7ea1f657b2a` with Deezer album `12047952`; all 17
tracks retained both provider track IDs. Live Zanmu release
`c3bcc159-20da-4e1e-bb9e-53f63dc32280` combined free iTunes collection
`1754263364`; all 16 tracks retained both IDs without an Apple developer token.
The old Discogs token is already in `.env.local` and is valid; the earlier
manual 403 was an empty-User-Agent probe, while the provider client sends the
required HeyaMetadata UA. The catalog/audio/ML/social roadmap is documented in
`docs/music-evidence-roadmap.md`.

Recording evidence is now live through migration 0012. Verified iTunes and
Deezer release-track matches expose preview URLs only in memory; `fpcalc -raw`
generates packed, versioned Chromaprint evidence on the canonical recording and
the temporary audio is deleted. Deezer track previews are renewed immediately
before use because album-response cache entries outlive signed media tokens.
The public read is `GET /api/v2/recordings/{id}/fingerprints`, encoded as
`base64-uint32le` with source checksum, provider track, duration, hash count,
algorithm, and generator versions. Live Zanmu produced 16/16 Apple
fingerprints; Abbey Road produced 17/17 Deezer fingerprints.

LRCLIB exact-signature enrichment uses its bounded `/api/get-cached` route in
the release job, the common Redis/S3 observation cache, a six-second request
ceiling, and four-way concurrency. Provider slowness and misses are partial
evidence and cannot fail the MusicBrainz spine. Plain, synchronized, and
instrumental results are stored with LRCLIB record ID, checksum, observation,
and retrieval time and exposed at `GET /api/v2/recordings/{id}/lyrics`. Live
Abbey Road stored 15 synchronized records. LRCLIB's potentially fan-out
`/api/get` route now runs only as an internal `recording_evidence_refresh_v1`
job on a single-worker `background` queue. The adaptive scheduler prioritizes
demanded recordings, while misses wait 30 days and transient failures back off.
There is no public evidence-refresh trigger.

Standalone recording discovery and ingestion are implemented through the
existing generic discovery/resolution surfaces; no recording-specific POST API
was added. Discovery ranks MusicBrainz recording candidates using artist names
and MBIDs, duration, ISRC, and release hints. Live `普変` without release hints
remained correctly ambiguous; adding ano, 220000 ms, and `猫猫吐吐:2023`
selected recording `72feb5de-7912-4ad4-b507-21a1d5e199fd` at 0.96. Standalone
ingestion produced Heya entity `b7daca1c-c2bf-47d5-9333-3d42071e5aa8` with
artist credit, duration, ISRC, rating, five release appearances, and Spotify
link. A subsequent ingestion of its single release preserved all five
appearances, the rating, and the link. Canonical entity/evidence reads now feed
the buffered access counter so adaptive MusicBrainz and LRCLIB scheduling is
actually demand-aware.

## Suggested next turn

1. Add bounded derived image variants (WebP/AVIF) and class-aware original
   retention before materializing high-volume profile catalogs.
2. Add conflict-safe AcoustID lookup and authenticated client fingerprint
   observations without exposing generic enrichment controls.
3. Expand TV/Anime discovery verification with provider-specific episode and
   season hints, then add credits/content ratings without weakening identity.

The previous repositories may be inspected for provider knowledge and metadata
coverage, but should not be copied as architectural constraints.

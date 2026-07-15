# Heya Metadata

Clean-slate metadata service for Heya.

The intended system architecture is documented in
[`HeyaMetadataV2.md`](./HeyaMetadataV2.md). Domain implementation begins with
the [movie vertical slice](./docs/domains/movie.md), backed by the executable
[semantic coverage catalog](./coverage/README.md).

## Development

Requires Go 1.26.4 or newer, Bun, Docker,
[mprocs](https://github.com/pvolok/mprocs), and
[Air](https://github.com/air-verse/air). On macOS:

```bash
brew install mprocs go-air
```

```bash
cp .env.example .env.local
# Add RustFS and provider credentials to .env.local.
make dev
```

`make dev` starts the local Postgres and Redis containers, applies application
and River migrations, builds the shared Go binary, and then opens mprocs. S3 is
the existing `heyamedia` bucket through `https://s3-api.karbowiak.dk`; v2's
metadata lives under `data/` while the existing `images/` keys remain reusable.
It is deliberately not emulated by another local container.

Open <http://127.0.0.1:3030>. The development stack has one stable public
origin and two private hot-reloading services:

| Port | Process | Purpose |
| --- | --- | --- |
| `3030` | `heya-metadata dev-proxy` | Stable browser-facing proxy |
| `3031` | Go API under Air | Rebuilds and restarts on Go changes |
| — | River worker under Air | Restarts from the same rebuilt Go binary |
| `3032` | Nuxt/Vite | Frontend HMR |

The proxy sends `/api`, `/api/*`, and the public connectivity routes under
`/v1/*` to Go and everything else to Nuxt. It stays running while Air replaces
the backend. In the `mprocs` UI, press `r` on the proxy pane when changing the
proxy implementation itself.

The Nuxt app is the Metadata Observatory: a development workbench for local
query search, identifier-first discovery, optional candidate resolution, River
progress, localized presentation, artwork, provenance, ratings, external IDs,
raw canonical JSON, provider refreshes, and in-memory request-scoped credentials. See
[`docs/metadata-observatory.md`](./docs/metadata-observatory.md).

The `dev-web` target raises the child process file-descriptor limit to avoid
Nuxt/Vite watcher failures on macOS shells that still default to 256 files.

Development URLs:

- Frontend: <http://127.0.0.1:3030>
- API docs: <http://127.0.0.1:3030/api/docs>
- OpenAPI: <http://127.0.0.1:3030/api/openapi.json>
- Liveness: <http://127.0.0.1:3030/api/v2/health/live>
- Readiness: <http://127.0.0.1:3030/api/v2/health/ready>
- Search: <http://127.0.0.1:3030/api/v2/search?q=matrix>
- Changes: <http://127.0.0.1:3030/api/v2/changes> (persist both `stream_id` and `next_cursor`)
- Observed public IP: <http://127.0.0.1:3030/v1/ip> (requires a trusted proxy header in local development)

The server also exposes the source-IP-only outside-in probe used by Heya's
remote-access setup. Its contract and proxy configuration are documented in
[`docs/connectivity-check.md`](./docs/connectivity-check.md).

Run individual processes in separate terminals when needed:

```bash
make dev-front
make dev-go
make dev-worker
make dev-web
```

`make dev` checks ports `3030`–`3032` and refuses to kill an existing
listener. Stop the reported process before retrying.

### Platform services

Postgres listens on `127.0.0.1:5441` and Redis on `127.0.0.1:6380`. Their data
is retained in named Docker volumes. Common commands are:

```bash
make infra-up       # start Postgres/Redis and apply all migrations
make infra-status
make migrate-status
make worker         # run the River worker without Air
make smoke          # verify River + Postgres + Redis + S3 end to end
make generate-api   # refresh api/openapi.yaml and the generated Go SDK
make acceptance     # verify the complete cross-domain public contract
make check-generated # fail if the checked-in contract or SDK has drifted
make movie-ingest TMDB_ID=603
make artist-ingest MUSICBRAINZ_ID=b10bbbfc-cf9e-42e0-be17-e2c3e1d2600d
make release-group-ingest MUSICBRAINZ_ID=9162580e-5df4-32de-80cc-f45a8d8a9b1d
make release-ingest MUSICBRAINZ_ID=044d87f2-9fda-475a-b041-47df9443a3f5
make musical-work-ingest OPENOPUS_ID=16406
make retention-sweep
make infra-down
```

Issued music releases can be ingested independently from their release group:

```bash
go run ./cmd/heya-metadata release ingest \
  --musicbrainz 044d87f2-9fda-475a-b041-47df9443a3f5 \
  --wait 90s
```

This stores complete media and track placement data and creates reusable
canonical recording entities for referenced MusicBrainz recordings.

Standalone recordings use the same entity kind and can be discovered with
structured artist, duration, ISRC, and release hints, then ingested directly:

```bash
go run ./cmd/heya-metadata discover recording --query '普変' \
  --artist ano --duration-ms 220000 --release '猫猫吐吐:2023'

go run ./cmd/heya-metadata recording ingest \
  --musicbrainz 72feb5de-7912-4ad4-b507-21a1d5e199fd --wait 90s
```

Books use separate work, edition, and author identities:

```bash
go run ./cmd/heya-metadata discover book --query 'The Hobbit' \
  --author 'J. R. R. Tolkien' --year 1937
go run ./cmd/heya-metadata book ingest --openlibrary OL27482W \
  --edition OL33891772M --wait 3m
```

The optional edition key forces one client-selected edition through the bounded
work catalog without crawling every edition of a popular work.

Composed musical works are separate from recordings, performances, and
releases. Open Opus is the first canonical spine:

```bash
go run ./cmd/heya-metadata discover musical-work \
  --query 'Symphony no. 5' --composer 'Ludwig van Beethoven' \
  --catalogue 'op. 67'
go run ./cmd/heya-metadata musical-work ingest --openopus 16406 --wait 90s
```

LRCLIB's slow `/api/get` lookup is an internal scheduled job on a single-worker
background queue. It never blocks release ingestion, and there is deliberately
no public endpoint for starting evidence refreshes.

The smoke command enqueues a real River job and waits for the separate worker.
It writes an immutable, gzip-compressed, content-addressed observation to S3,
round-trips a temporary Redis value, and records the observation and completion
atomically in Postgres.

The API's liveness endpoint only reports whether the process is alive.
Readiness probes Postgres, Redis, and S3 concurrently and returns `503` if any
dependency is unavailable. Dependency error details stay in debug logs rather
than the public response.

### Movie ingestion

The first complete vertical slice collects TMDB movie detail and collection
responses, stores each response as an immutable observation, normalizes the
provider record, resolves opaque canonical identity, combines retained source
records, and atomically updates detail/summary documents, provenance, search,
provider freshness, and the public change outbox.

```bash
# Run `make dev` first so the River worker is available.
make movie-ingest TMDB_ID=603

curl http://127.0.0.1:3030/api/v2/search?q=matrix
curl 'http://127.0.0.1:3030/api/v2/changes?after=0&limit=500'

# Provider-backed identity discovery with structured disambiguation hints.
go run ./cmd/heya-metadata discover artist --query ano --country JP \
  --type person --release '猫猫吐吐:2023'

go run ./cmd/heya-metadata discover movie --query Dune --year 2021 \
  --language en

go run ./cmd/heya-metadata discover release-group --query 'Abbey Road' \
  --artist 'The Beatles' --year 1969 --type album --track 'Come Together'

go run ./cmd/heya-metadata discover tv --query 'Game of Thrones' --year 2011 \
  --network HBO --episode 'Winter Is Coming'

go run ./cmd/heya-metadata discover anime --query 'Cowboy Bebop' --year 1998 \
  --type tv_series --episode-count 26 --episode 'Asteroid Blues'
```

Provider adapters declare accepted identifiers, supplied metadata scopes, and
raw-response retention. The reusable mixer uses those declarations to plan
eligible collectors as new external IDs are discovered. Provider-specific
normalizers feed a deterministic domain combiner; consumers receive one merged
canonical document rather than separate provider payloads.

Release ingestion also derives raw Chromaprint evidence from verified
iTunes/Deezer previews when `fpcalc` is installed, and performs LRCLIB
exact-signature lyric lookups. Evidence is available at
`GET /api/v2/recordings/{id}/fingerprints` and
`GET /api/v2/recordings/{id}/lyrics`; preview audio is never retained, and
signed URLs are not copied beyond the existing 48-hour raw provider evidence.
On macOS, `brew install chromaprint` provides `fpcalc`.

`POST /api/v2/fingerprint-matches` accepts raw `base64-uint32le` evidence for
indexed local matching and/or a compressed Chromaprint for AcoustID. The
short-lived River run erases submitted fingerprints after completion. Full TV
and movie metadata is available through paginated
`GET /api/v2/entities/{id}/credits` and `/ratings`; ordinary detail embeds at
most 50 credits.

`GET /api/v2/search` is the low-latency canonical index for query-only matching
and accepts a `kind` filter. Warm results are served from Redis; upstream
providers never block this route. When identifiers are available, submit all of
them directly to `POST /api/v2/discoveries`; query-only callers use search first
and discovery on a miss. Identical normalized discovery requests share a
high-priority River job and six-hour result. A unique result returns
`result.entity_id` for a direct canonical read. Ambiguity returns confidence,
evidence, and opaque `candidate_ref` values; only then does the client call
`/api/v2/resolutions`.
Movie, Artist, Release Group, Recording, Musical Work, TV Show, Anime, and Book
Work have provider-backed discovery.
TV and Anime retain separate canonical kinds, jobs, tables, and API families.
The complete consuming-server state machine, including both asynchronous poll
boundaries, is documented in
[Canonical entity lookup and resolution flow](./docs/client-resolution-flow.md).

MusicBrainz, Apple/iTunes, and Deezer artist IDs now run through the canonical
artist pipeline. MusicBrainz relationships can unlock supplemental Apple,
Deezer, Discogs, Last.fm, and Wikidata evidence, but an Apple- or Deezer-only
artist does not need a MusicBrainz record to receive a Heya UUID and public
discography. Names alone never merge artists. Providers and music entity kinds
whose canonical merge is not implemented yet still run through River, the
shared exact-response cache, Postgres observations, and expiring S3 evidence.
The generic collector CLI takes the collector separately from the identifier
source, which is useful for supplemental sources such as Last.fm:

```bash
go run ./cmd/heya-metadata provider collect \
  --provider musicbrainz --namespace artist \
  --value b10bbbfc-cf9e-42e0-be17-e2c3e1d2600d

go run ./cmd/heya-metadata provider collect \
  --provider lastfm --id-provider musicbrainz --namespace artist \
  --value b10bbbfc-cf9e-42e0-be17-e2c3e1d2600d

go run ./cmd/heya-metadata artist ingest --apple 591024034
go run ./cmd/heya-metadata artist ingest --deezer 5287498
```

`--api-key` hands a caller token to the worker through the same short-lived,
opaque Redis reference used by the API; plaintext never enters River or
Postgres. Supported source collectors are `anidb`, `apple`, `deezer`, `discogs`,
`lastfm`, `musicbrainz`, `openopus`, `tvmaze`, and `wikidata`.

Raw provider bytes are written beneath lifecycle-specific prefixes. RustFS
expires `data/ephemeral/24h/` after one day and `data/ephemeral/48h/` after two
days; neither rule can match `images/` or permanent data. TMDB currently uses
the 48-hour tier. Small Postgres observation metadata and durable normalized
records remain, so canonical data and provenance continue to work afterward.

Workers enqueue an hourly reconciliation sweep with a 24-hour grace period. It
performs an idempotent fallback delete if the RustFS lifecycle scanner is
delayed, then marks the blob deleted in Postgres. `make retention-sweep` is the
manual equivalent. The exported live rules and recovery instructions are in
[`ops/rustfs`](./ops/rustfs/README.md).

Canonical documents expose opaque image IDs. `GET /api/v2/images/{id}` queues
the unique `image_materialize_v1` job on first use and returns `202`; once the
bounded, allowlisted source fetch has been MIME-validated and stored beneath
`data/images/original/`, the same route serves the immutable image bytes.

### Development cache

Air always overwrites `.dev/air/heya-metadata`; it does not retain a binary per
rebuild. The running process can keep the previous binary inode open while Go
atomically installs the next one, so only the running and newly built versions
can coexist briefly.

Go's compiler cache is not a collection of complete application builds, so Air
cannot retain exactly its last two entries. Air uses the isolated
`.cache/go-build-air` cache; normal CLI/tests use `.cache/go-build`, so pruning
after an Air build cannot invalidate a concurrent test or SDK generation. The
Air cache is cleared if it exceeds `GO_CACHE_MAX_MB`, which defaults to 512 MiB:

```bash
make dev-cache-status
make dev GO_CACHE_MAX_MB=256
make dev-clean
```

The module download cache is separate at `.cache/go-mod`: it is stable across
rebuilds and is intentionally retained by `make dev-clean` so dependencies are
not downloaded repeatedly. Air is installed as a standalone tool rather than
added to `go.mod`; current Air releases otherwise pull a large development-only
dependency graph into the application's module cache.

### Direct CLI use

The production-shaped server still defaults to `:3030` when run directly:

```bash
go run ./cmd/heya-metadata
go run ./cmd/heya-metadata serve
```

Useful commands:

```bash
go run ./cmd/heya-metadata version
go run ./cmd/heya-metadata openapi-spec --format yaml
go run ./cmd/heya-metadata migrate up
go run ./cmd/heya-metadata migrate status
go run ./cmd/heya-metadata worker
go run ./cmd/heya-metadata smoke
go run ./cmd/heya-metadata movie ingest --tmdb 603
go run ./cmd/heya-metadata artist ingest --musicbrainz b10bbbfc-cf9e-42e0-be17-e2c3e1d2600d
go run ./cmd/heya-metadata release-group ingest --musicbrainz 9162580e-5df4-32de-80cc-f45a8d8a9b1d
go run ./cmd/heya-metadata retention sweep
go test ./...
```

The checked-in OpenAPI contract, generated Go client, and contract acceptance
workflow are documented in [`docs/api-contract.md`](./docs/api-contract.md).

Configuration is read from process environment variables after `.env.local`
and `.env` are loaded; process variables always win. See
[`.env.example`](./.env.example). Never commit the S3 credentials.

Provider collectors share exact-response caching, distributed fetch locking,
raw evidence storage, and transient caller-credential handoff. See the
[provider blueprint](./docs/providers.md). TMDB resolution and refresh requests
accept an optional `X-Heya-TMDB-API-Key`; the plaintext key is held temporarily
in Redis and is never stored in River or Postgres. Interactive work is promoted
above stale-on-read and adaptive background refreshes; entity demand decays into
a 2/7/14/30-day refresh cadence.

Canonical artist resolution accepts request-scoped `X-Heya-Apple-API-Key`,
`X-Heya-Discogs-API-Key`, and `X-Heya-LastFM-API-Key` headers as well. OMDb is
the second movie collector and accepts `X-Heya-OMDB-API-Key` through the
same mechanism. TMDB-discovered IMDb IDs unlock OMDb plot/runtime evidence and
independent IMDb, Rotten Tomatoes, and Metacritic rating scales.

TVDB follows the same discovered-ID path through IMDb remote-ID search and
accepts `X-Heya-TVDB-API-Key`. Its extended movie evidence contributes durable
TVDB identity, translations, classifications, companies, credits, releases,
certifications, and artwork.

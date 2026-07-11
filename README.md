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

The proxy sends `/api` and `/api/*` to Go and everything else to Nuxt. It stays
running while Air replaces the backend. In the `mprocs` UI, press `r` on the
proxy pane when changing the proxy implementation itself.

The `dev-web` target raises the child process file-descriptor limit to avoid
Nuxt/Vite watcher failures on macOS shells that still default to 256 files.

Development URLs:

- Frontend: <http://127.0.0.1:3030>
- API docs: <http://127.0.0.1:3030/api/docs>
- OpenAPI: <http://127.0.0.1:3030/api/openapi.json>
- Liveness: <http://127.0.0.1:3030/api/v2/health/live>
- Readiness: <http://127.0.0.1:3030/api/v2/health/ready>
- Search: <http://127.0.0.1:3030/api/v2/search?q=matrix>
- Changes: <http://127.0.0.1:3030/api/v2/changes>

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
make movie-ingest TMDB_ID=603
make retention-sweep
make infra-down
```

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
curl http://127.0.0.1:3030/api/v2/changes?after=0
```

Provider adapters declare accepted identifiers, supplied metadata scopes, and
raw-response retention. The reusable mixer uses those declarations to plan
eligible collectors as new external IDs are discovered. Provider-specific
normalizers feed a deterministic domain combiner; consumers receive one merged
canonical document rather than separate provider payloads.

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

### Development cache

Air always overwrites `.dev/air/heya-metadata`; it does not retain a binary per
rebuild. The running process can keep the previous binary inode open while Go
atomically installs the next one, so only the running and newly built versions
can coexist briefly.

Go's compiler cache is not a collection of complete application builds, so Air
cannot retain exactly its last two entries. Development uses an isolated
`.cache/go-build` instead. After every successful Air build it is cleared if it
exceeds `GO_CACHE_MAX_MB`, which defaults to 512 MiB:

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
go run ./cmd/heya-metadata retention sweep
go test ./...
```

Configuration is read from process environment variables after `.env.local`
and `.env` are loaded; process variables always win. See
[`.env.example`](./.env.example). Never commit the S3 credentials.

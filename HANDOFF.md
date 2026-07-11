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
  and raw-blob retention. The mixer can re-plan when a collector discovers IDs
  that unlock another provider. Domain combiners union provider evidence while
  applying explicit precedence only to scalar winners.
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

1. Add OMDb as the second movie collector, unlocked by accepted IMDb title
   claims discovered by TMDB.
2. Prove multi-provider combination with plot fallback and separate IMDb,
   Rotten Tomatoes, and Metacritic rating scales and provenance.
3. Add TVDB and Fanart.tv one at a time through the same capability-driven
   pipeline, expanding passing entries in `coverage/movie.json`.

The previous repositories may be inspected for provider knowledge and metadata
coverage, but should not be copied as architectural constraints.

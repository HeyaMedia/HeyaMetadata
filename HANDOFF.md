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
passing baseline, and the first domain-design milestone is present.

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
```

The intended initial command surface is:

```text
heya-metadata
heya-metadata serve
heya-metadata version
heya-metadata openapi-spec
```

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
- In restricted Codex environments, set `GOPATH` and `GOCACHE` under `/tmp` so
  Go does not try to write outside the workspace.

## Suggested next turn

1. Read this handoff and the architecture document.
2. Read `docs/domains/movie.md` and `coverage/movie.json`.
3. Add local development infrastructure for Postgres, Redis, and an
   S3-compatible RustFS stand-in, keeping API and worker processes separate.
4. Establish migrations, required Postgres extensions, River, dependency-aware
   readiness, and configuration validation.
5. Implement the first TMDB movie milestone through the complete observation,
   blob, normalization, identity, merge, projection, search, cache, and change
   pipeline described in the movie design.

The previous repositories may be inspected for provider knowledge and metadata
coverage, but should not be copied as architectural constraints.

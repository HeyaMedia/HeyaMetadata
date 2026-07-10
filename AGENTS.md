# Heya Metadata repository guidance

## Authority

- `HeyaMetadataV2.md` is the architectural source of truth.
- This is a clean-slate service. Code in `../HeyaMedia` may be inspected for
  provider knowledge and metadata coverage, but its storage, API, identifiers,
  and behavior are not compatibility requirements.
- Prefer complete vertical slices over speculative shared abstractions.

## Go conventions

- The module is `github.com/HeyaMedia/HeyaMetadata` and the binary is
  `heya-metadata`.
- Use the standard library unless a dependency materially improves the design.
- Pass `context.Context` through request, database, blob, and provider paths.
- Keep durable work in River jobs; API handlers may enqueue and briefly wait but
  must not run provider pipelines themselves.
- Wrap errors with useful operation context. Use `slog` for structured logs.
- Add tests for identity, retry/idempotency, projection-version, and cursor
  invariants rather than testing implementation details.

## Validation

Run before handing off a change:

```text
go fmt ./...
go test ./...
go build ./...
```

The coverage catalog under `coverage/` is executable product scope. Changes to
public metadata behavior must update it and preserve provenance expectations.

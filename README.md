# Heya Metadata

Clean-slate metadata service for Heya.

The intended system architecture is documented in
[`HeyaMetadataV2.md`](./HeyaMetadataV2.md). Domain implementation begins with
the [movie vertical slice](./docs/domains/movie.md), backed by the executable
[semantic coverage catalog](./coverage/README.md).

## Development

Requires Go 1.26.4 or newer.

```bash
go run ./cmd/heya-metadata
go run ./cmd/heya-metadata serve
```

The server listens on `:3030` by default:

- API docs: <http://localhost:3030/api/docs>
- OpenAPI: <http://localhost:3030/api/openapi.json>
- Liveness: <http://localhost:3030/api/v2/health/live>
- Readiness: <http://localhost:3030/api/v2/health/ready>

Useful commands:

```bash
go run ./cmd/heya-metadata version
go run ./cmd/heya-metadata openapi-spec --format yaml
go test ./...
```

Configuration is read from environment variables. See [`.env.example`](./.env.example).

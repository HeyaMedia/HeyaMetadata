# Heya Metadata

Clean-slate metadata service for Heya.

The intended system architecture is documented in
[`HeyaMetadataV2.md`](./HeyaMetadataV2.md). Domain implementation begins with
the [movie vertical slice](./docs/domains/movie.md), backed by the executable
[semantic coverage catalog](./coverage/README.md).

## Development

Requires Go 1.26.4 or newer, Bun,
[mprocs](https://github.com/pvolok/mprocs), and
[Air](https://github.com/air-verse/air). On macOS:

```bash
brew install mprocs go-air
```

```bash
make dev
```

Open <http://127.0.0.1:3030>. The development stack has one stable public
origin and two private hot-reloading services:

| Port | Process | Purpose |
| --- | --- | --- |
| `3030` | `heya-metadata dev-proxy` | Stable browser-facing proxy |
| `3031` | Go API under Air | Rebuilds and restarts on Go changes |
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

Run individual processes in separate terminals when needed:

```bash
make dev-front
make dev-go
make dev-web
```

`make dev` checks ports `3030`–`3032` and refuses to kill an existing
listener. Stop the reported process before retrying.

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
go test ./...
```

Configuration is read from environment variables. See [`.env.example`](./.env.example).

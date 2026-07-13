# Container deployment

The repository publishes a multi-architecture image to:

```text
ghcr.io/heyamedia/heyametadata:latest
```

The image contains the Go service, the compiled Nuxt observatory, and
Chromaprint's `fpcalc`. Its default command is `serve`; the API and SPA share
port `3030`.

## Required processes

Use the same image for all three production roles:

```sh
# Run once for each deployed version before starting the new service.
docker run --rm --env-file .env ghcr.io/heyamedia/heyametadata:latest migrate up

# Public API and observatory.
docker run --env-file .env -p 3030:3030 ghcr.io/heyamedia/heyametadata:latest

# Durable River workers. Run at least one replica.
docker run --env-file .env ghcr.io/heyamedia/heyametadata:latest worker
```

The API and workers must use the same PostgreSQL, Redis, S3, provider, and
captcha configuration. Supply credentials through the deployment platform's
secret/environment facility; never bake an environment file into the image.
Start from `.env.example` and set at least the production database, Redis, and
S3 values.

## Operations

- Liveness: `GET /api/v2/health/live`
- Dependency readiness: `GET /api/v2/health/ready`
- API reference: `/api/docs`
- OpenAPI: `/api/openapi.yaml`
- Default container port: `3030`
- Default command: `serve`
- Worker command: `worker`
- Migration command: `migrate up`

Container images are built only when a semantic version tag matching `v*` is
pushed. Each release is built natively on separate amd64 and arm64 GitHub
runners, then published as one multi-architecture manifest under `latest`, the
full semantic version, the major/minor version, and the Git SHA. Ordinary main
branch pushes and pull requests run verification without building an image.
Production deployments should pin a semantic version or image digest.

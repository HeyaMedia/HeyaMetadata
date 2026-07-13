# syntax=docker/dockerfile:1.7

ARG GO_VERSION=1.26.5
ARG BUN_VERSION=1.3.14

FROM oven/bun:${BUN_VERSION}-alpine AS web-build
WORKDIR /src/web
COPY web/package.json web/bun.lock ./
RUN bun install --frozen-lockfile
COPY web/ ./
RUN bun run generate

FROM golang:${GO_VERSION}-alpine3.23 AS go-build
WORKDIR /src
RUN apk add --no-cache ca-certificates
COPY go.mod go.sum ./
RUN go mod download
COPY . ./

ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -buildvcs=false -trimpath \
    -ldflags="-s -w -X github.com/HeyaMedia/HeyaMetadata/internal/buildinfo.Version=${VERSION} -X github.com/HeyaMedia/HeyaMetadata/internal/buildinfo.Commit=${COMMIT} -X github.com/HeyaMedia/HeyaMetadata/internal/buildinfo.BuildDate=${BUILD_DATE}" \
    -o /out/heya-metadata ./cmd/heya-metadata

FROM alpine:3.23 AS runtime

ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown
LABEL org.opencontainers.image.title="HeyaMetadata" \
      org.opencontainers.image.description="Canonical, provenance-aware metadata for Heya media servers" \
      org.opencontainers.image.source="https://github.com/HeyaMedia/HeyaMetadata" \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${COMMIT}" \
      org.opencontainers.image.created="${BUILD_DATE}"

RUN apk add --no-cache ca-certificates chromaprint tzdata \
    && addgroup -S -g 10001 heya \
    && adduser -S -D -H -u 10001 -G heya heya

WORKDIR /app
COPY --from=go-build /out/heya-metadata /usr/local/bin/heya-metadata
COPY --from=web-build --chown=10001:10001 /src/web/.output/public /app/web

ENV HEYA_METADATA_HOST=0.0.0.0 \
    HEYA_METADATA_PORT=3030 \
    HEYA_METADATA_WEB_ROOT=/app/web \
    HEYA_METADATA_FPCALC_PATH=/usr/bin/fpcalc \
    HEYA_METADATA_LOG_FORMAT=json

EXPOSE 3030
USER 10001:10001
ENTRYPOINT ["/usr/local/bin/heya-metadata"]
CMD ["serve"]
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -q -O /dev/null http://127.0.0.1:3030/api/v2/health/live || exit 1

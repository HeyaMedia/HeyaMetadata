GO_CACHE_DIR ?= $(CURDIR)/.cache/go-build
GO_MODCACHE_DIR ?= $(CURDIR)/.cache/go-mod
GO_CACHE_MAX_MB ?= 512
GO := GOCACHE=$(GO_CACHE_DIR) GOMODCACHE=$(GO_MODCACHE_DIR) go

.PHONY: build fmt test dev dev-front dev-go dev-web air-build web-install dev-cache-status dev-clean

build:
	$(GO) build ./...

fmt:
	$(GO) fmt ./...

test:
	$(GO) test ./...

# Stable public proxy :3030 + Air-managed Go API :3031 + Nuxt/Vite :3032.
# Unlike the old Heya preflight, this refuses to kill an unrelated listener.
dev:
	@command -v mprocs >/dev/null 2>&1 || { echo "mprocs not found — install with: brew install mprocs"; exit 1; }
	@command -v air >/dev/null 2>&1 || { echo "Air not found — install with: brew install go-air"; exit 1; }
	@air -v 2>&1 | grep -q "built with Go" || { echo "the 'air' on PATH is not air-verse/air — install with: brew install go-air"; exit 1; }
	@command -v bun >/dev/null 2>&1 || { echo "bun not found — install from: https://bun.sh"; exit 1; }
	@command -v lsof >/dev/null 2>&1 || { echo "lsof is required for the development port preflight"; exit 1; }
	@tools/dev/check-ports.sh 3030 3031 3032
	@test -d web/node_modules || $(MAKE) web-install
	@mkdir -p .dev/bin .dev/air $(GO_CACHE_DIR) $(GO_MODCACHE_DIR)
	mprocs

dev-front:
	@mkdir -p .dev/bin $(GO_CACHE_DIR) $(GO_MODCACHE_DIR)
	$(GO) build -o .dev/bin/heya-metadata-dev-proxy ./cmd/heya-metadata
	exec .dev/bin/heya-metadata-dev-proxy dev-proxy

dev-go:
	@mkdir -p .dev/air $(GO_CACHE_DIR) $(GO_MODCACHE_DIR)
	GOCACHE=$(GO_CACHE_DIR) GOMODCACHE=$(GO_MODCACHE_DIR) air

dev-web:
	@ulimit -n 65536; cd web && bun run dev

air-build:
	@mkdir -p .dev/air $(GO_CACHE_DIR) $(GO_MODCACHE_DIR)
	$(GO) build -o .dev/air/heya-metadata ./cmd/heya-metadata
	@tools/dev/prune-go-cache.sh "$(GO_CACHE_DIR)" "$(GO_CACHE_MAX_MB)"

web-install:
	cd web && bun install --frozen-lockfile

dev-cache-status:
	@du -sh $(GO_CACHE_DIR) $(GO_MODCACHE_DIR) 2>/dev/null || true

dev-clean:
	rm -rf .dev $(GO_CACHE_DIR)

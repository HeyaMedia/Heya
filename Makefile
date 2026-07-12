GOBIN := $(shell go env GOPATH)/bin
GO_CACHE_DIR ?= $(CURDIR)/.cache/go-build
GO_MODCACHE_DIR ?= $(CURDIR)/.cache/go-mod
GO := GOCACHE=$(GO_CACHE_DIR) GOMODCACHE=$(GO_MODCACHE_DIR) go

.PHONY: build run test lint clean db-up db-down db-reset migrate build-frontend dev dev-front dev-go dev-web gen-api-client gen-heyamedia-client deadcode dead-components docker-runtime-cpu docker-runtime-cuda docker-runtime-openvino docker docker-cuda docker-openvino docker-multiarch docker-run docker-run-gpu

# Pinned at the same version HeyaMedia uses for its self-client; oapi-codegen
# bumps occasionally break field shapes and we want clients to match.
OAPI_CODEGEN := github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.4.1
HEYAMEDIA_URL ?= https://heya.media
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

build-frontend:
	cd web && bun install && NUXT_PUBLIC_HEYA_VERSION=$(VERSION) bun run build
	rm -rf web/dist
	mkdir -p web/dist
	cp -r web/.output/public/* web/dist/
	touch web/dist/.gitkeep

build: build-frontend
	$(GO) build -tags embed_frontend -ldflags="-X github.com/karbowiak/heya/internal/ui.Version=$(VERSION)" -o bin/heya ./cmd/heya

build-go:
	$(GO) build -ldflags="-X github.com/karbowiak/heya/internal/ui.Version=$(VERSION)" -o bin/heya ./cmd/heya

run: build-go
	./bin/heya serve

# Dev: `heya dev-proxy` on :8080 is the stable front door — it fronts Nuxt
# (:3000) + the air-run backend (:3050) and owns the Tailscale node, so air
# rebuilds of the backend never drop the front door, the tailnet node, or the
# browser's HMR/WS sockets. mprocs supervises all three and tears them down
# cleanly on quit (q / Ctrl+C). The preflight reclaims :8080/:3050/:3000 from
# anything a previous hard kill left orphaned. Open http://localhost:8080.
#
# mprocs is the prerequisite: `brew install mprocs`.
dev:
	@command -v mprocs >/dev/null 2>&1 || { echo "mprocs not found — install with: brew install mprocs"; exit 1; }
	@mkdir -p tmp  # gitignored; the proxy + air both build into it — create it before mprocs spawns them
	@for p in 8080 3050 3000; do pids=$$(lsof -ti tcp:$$p 2>/dev/null); [ -n "$$pids" ] && kill $$pids 2>/dev/null || true; done
	mprocs

# Same trio as `make dev`, split across terminals if you want separate control.
dev-front:
	mkdir -p tmp && $(GO) build -o tmp/heya-dev ./cmd/heya && exec tmp/heya-dev dev-proxy

dev-go:
	mkdir -p tmp && $(GO) run github.com/air-verse/air@latest

dev-web:
	cd web && bun run dev

test:
	$(GO) test ./...

test-unit:
	$(GO) test -short -count=1 ./...

test-integration:
	$(GO) test -count=1 ./...

test-coverage:
	$(GO) test -coverprofile=coverage.out ./... && $(GO) tool cover -html=coverage.out -o coverage.html

lint:
	$(GO) vet ./...

# Report Go dead-code candidates via x/tools' deadcode analyzer. Needs
# network on the first run (go run downloads the tool). Output is a list of
# CANDIDATES for manual review — reflection, build tags, and CLI-only paths
# produce false positives — so this is a report, not an error gate.
deadcode:
	$(GO) run golang.org/x/tools/cmd/deadcode@latest ./...

# Report Vue components under web/app/components with zero references in
# web/app + web/shared (PascalCase/kebab tags, Lazy prefix, imports,
# resolveComponent strings). Candidates for manual review, not an error gate.
dead-components:
	cd web && bun ../tools/dead-components.ts

clean:
	rm -rf bin/
	rm -rf .cache/go-build .cache/go-build-air
	rm -rf web/.output web/.nuxt web/node_modules

db-up:
	docker compose up -d postgres

db-down:
	docker compose down

db-reset: build-go
	docker compose down
	rm -rf data/postgres
	docker compose up -d postgres
	@echo "Waiting for postgres..."
	@sleep 2

reset: build-go
	docker compose down
	rm -rf data/*
	docker compose up -d postgres
	@echo "Waiting for postgres..."
	@sleep 2

migrate:
	goose -dir migrations postgres "$(DATABASE_URL)" up

migrate-down:
	goose -dir migrations postgres "$(DATABASE_URL)" down

# Regenerate the committed OpenAPI spec. Run any time you add or change a
# `*_huma.go` handler. CI also runs this and fails on drift, so a forgotten
# regen turns into a red build, not a silent skew.
#
# The typed TS client (paths, components, $heya/useHeya) is generated from
# this spec at Nuxt build time by `nuxt-open-fetch` — no separate FE codegen
# step needed.
gen-api-client:
	go run ./cmd/heya openapi-spec --format json -o web/shared/api.openapi.json

# Regenerate the typed Go client for the upstream heya.media metadata
# server. The TS counterpart isn't wired — the FE only talks to Heya's
# own API, which proxies heya.media server-side. If the FE ever needs
# direct upstream types, add an `openapi-typescript` step here outputting
# to a non-auto-imported path (Nuxt would otherwise hit duplicate-symbol
# warnings with shared/types/api.gen.ts, which exports the same
# openapi-typescript top-level names).
#
# Fetches the 3.0 spec directly from HEYAMEDIA_URL — oapi-codegen v2.4.x
# needs 3.0; heya.media downgrades its 3.1 master server-side and exposes
# both. Spec snapshot gets committed so the build is reproducible without
# network. CI runs this + diffs to enforce regen-on-spec-bump.
gen-heyamedia-client:
	@command -v curl >/dev/null || { echo "curl is required"; exit 1; }
	curl -fsSL "$(HEYAMEDIA_URL)/api/openapi-3.0.json" -o clients/heyamedia/openapi-3.0.json
	go run $(OAPI_CODEGEN) \
		-config clients/heyamedia/cfg.yaml \
		clients/heyamedia/openapi-3.0.json

# Build the production container locally. Runtime images live in `.docker/`;
# the app image builds frontend/backend and copies only the Heya binary onto
# the selected runtime.
#
# Builds for the host arch by default — fast and native (no QEMU) on both
# Apple Silicon and Linux amd64. The CPU runtime is arch-agnostic (it derives
# the per-arch bits from dpkg), so override PLATFORM to cross-build a single
# arch, e.g. `make docker PLATFORM=linux/amd64`. NOTE: the Nuxt prerender step
# is much slower under bun on amd64 than arm64 — an amd64 build can take several
# extra minutes at `bun run build`; this is a bun node-compat quirk, not a hang.
#
# The CPU runtime already does Intel + AMD **video transcode** via VAAPI/QSV —
# just pass the GPU at run time (see docker-run-gpu). The GPU **ONNX** runtimes
# (sonic-analysis) are separate amd64-only layers FROM the CPU runtime.
PLATFORM ?=
docker-runtime-cpu:
	docker build $(if $(PLATFORM),--platform=$(PLATFORM),) -f .docker/Dockerfile.cpu -t heya:runtime-cpu .

docker: docker-runtime-cpu
	docker build $(if $(PLATFORM),--platform=$(PLATFORM),) -f .docker/Dockerfile \
		--build-arg BASE_RUNTIME_IMAGE=heya:runtime-cpu -t heya:base .

docker-runtime-cuda:
	docker build --platform=linux/amd64 -f .docker/Dockerfile.cuda \
		--build-arg BASE_RUNTIME_IMAGE=heya:runtime-cpu -t heya:runtime-cuda .

docker-cuda: docker-runtime-cpu docker-runtime-cuda
	docker build --platform=linux/amd64 -f .docker/Dockerfile \
		--build-arg BASE_RUNTIME_IMAGE=heya:runtime-cuda -t heya:cuda .

docker-runtime-openvino:
	docker build --platform=linux/amd64 -f .docker/Dockerfile.openvino \
		--build-arg BASE_RUNTIME_IMAGE=heya:runtime-cpu -t heya:runtime-openvino .

docker-openvino: docker-runtime-cpu docker-runtime-openvino
	docker build --platform=linux/amd64 -f .docker/Dockerfile \
		--build-arg BASE_RUNTIME_IMAGE=heya:runtime-openvino -t heya:openvino .

# Build + push the base as one multi-arch manifest (what CI does on a tag).
# Requires a buildx builder with QEMU; multi-platform images can't be loaded
# into the local docker engine, so this pushes — set IMAGE to your registry ref.
IMAGE ?= heya:base
docker-multiarch:
	docker buildx build -f .docker/Dockerfile --platform=linux/amd64,linux/arm64 -t $(IMAGE) --push .

# Run the locally-built image against the docker-compose postgres on the
# host. Bind-mounts ./data so the Tailscale state + transcode cache survive.
docker-run:
	docker run --rm -it \
		-p 8080:8080 \
		-v $(PWD)/data:/data \
		-e HEYA_DATABASE_URL='postgres://heya:heya@host.docker.internal:5440/heya?sslmode=disable' \
		heya:base

# Run with the host GPU exposed for hardware transcode (Intel/AMD via /dev/dri).
# Adds the render group so the non-root paths can reach the device too.
docker-run-gpu:
	docker run --rm -it \
		-p 8080:8080 \
		-v $(PWD)/data:/data \
		--device /dev/dri:/dev/dri \
		--group-add "$$(getent group render | cut -d: -f3)" \
		-e HEYA_HWACCEL=vaapi \
		-e HEYA_DATABASE_URL='postgres://heya:heya@host.docker.internal:5440/heya?sslmode=disable' \
		heya:base

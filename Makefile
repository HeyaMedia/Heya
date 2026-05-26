GOBIN := $(shell go env GOPATH)/bin

.PHONY: build run test lint clean db-up db-down db-reset migrate build-frontend dev dev-proxy dev-go dev-web gen-api-client gen-heyamedia-client docker docker-run

# Pinned at the same version HeyaMedia uses for its self-client; oapi-codegen
# bumps occasionally break field shapes and we want clients to match.
OAPI_CODEGEN := github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.4.1
HEYAMEDIA_URL ?= https://heya.media

build-frontend:
	cd web && bun install && bun run build
	rm -rf web/dist/*
	cp -r web/.output/public/* web/dist/
	touch web/dist/.gitkeep

build: build-frontend
	go build -o bin/heya ./cmd/heya

build-go:
	go build -o bin/heya ./cmd/heya

run: build-go
	./bin/heya serve

# Dev: Caddy on :8080 fronts Nuxt (:3000) and heya serve (:3050). Air only
# rebuilds the Go binary — Caddy and Nuxt stay alive across restarts, so
# the browser's HMR socket and any in-flight WS connection never see the
# front door go away. Open http://localhost:8080. Ctrl+C kills all three.
#
# Caddy is the prerequisite: `brew install caddy`.
dev:
	@command -v caddy >/dev/null 2>&1 || { echo "caddy not found — install with: brew install caddy"; exit 1; }
	@trap 'kill 0' INT TERM; \
		caddy run --config Caddyfile.dev --adapter caddyfile & \
		go run github.com/air-verse/air@latest & \
		(cd web && bun run dev) & \
		wait

# Same trio as `make dev`, split if you want separate terminals.
dev-proxy:
	caddy run --config Caddyfile.dev --adapter caddyfile

dev-go:
	go run github.com/air-verse/air@latest

dev-web:
	cd web && bun run dev

test:
	go test ./...

test-unit:
	go test -short -count=1 ./...

test-integration:
	go test -count=1 ./...

test-coverage:
	go test -coverprofile=coverage.out ./... && go tool cover -html=coverage.out -o coverage.html

lint:
	go vet ./...

clean:
	rm -rf bin/
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

# Build the production container locally. Multi-stage: bun frontend ->
# go backend -> minideb runtime with jellyfin-ffmpeg + libonnxruntime.
# Tag is `heya:dev` so it doesn't collide with whatever ghcr.io pulls
# when you `docker pull` later.
#
# --platform=linux/amd64 because jellyfin-ffmpeg's apt repo only ships
# amd64 .debs. On Apple Silicon this means QEMU emulation (slow), on
# Linux amd64 it's a no-op. CI on GHCR runners matches natively.
docker:
	docker build --platform=linux/amd64 -t heya:dev .

# Run the locally-built image against the docker-compose postgres on the
# host. Bind-mounts ./data so the Tailscale state + transcode cache survive.
docker-run:
	docker run --rm -it \
		-p 8080:8080 \
		-v $(PWD)/data:/data \
		-e HEYA_DATABASE_URL='postgres://heya:heya@host.docker.internal:5440/heya?sslmode=disable' \
		heya:dev

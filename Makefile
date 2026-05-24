GOBIN := $(shell go env GOPATH)/bin

.PHONY: build run test lint clean db-up db-down db-reset migrate build-frontend dev gen-api-client

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

dev: build-go
	./bin/heya dev

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
	./bin/heya user create --username admin --email admin@localhost --password admin --admin

reset: build-go
	docker compose down
	rm -rf data/*
	docker compose up -d postgres
	@echo "Waiting for postgres..."
	@sleep 2
	./bin/heya user create --username admin --email admin@localhost --password admin --admin

migrate:
	goose -dir migrations postgres "$(DATABASE_URL)" up

migrate-down:
	goose -dir migrations postgres "$(DATABASE_URL)" down

# Regenerate the committed OpenAPI spec + typed TS client. Run any time you
# add or change a `*_huma.go` handler. CI also runs this and fails on drift,
# so a forgotten regen turns into a red build, not a silent skew.
gen-api-client:
	go run ./cmd/heya openapi-spec --format json -o web/shared/api.openapi.json
	cd web && bunx --bun openapi-typescript shared/api.openapi.json -o shared/types/api.gen.ts

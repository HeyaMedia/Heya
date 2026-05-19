.PHONY: build run test lint clean db-up db-down migrate build-frontend dev

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

migrate:
	goose -dir migrations postgres "$(DATABASE_URL)" up

migrate-down:
	goose -dir migrations postgres "$(DATABASE_URL)" down

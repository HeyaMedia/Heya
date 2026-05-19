.PHONY: build run test lint clean db-up db-down migrate build-frontend dev-frontend

build-frontend:
	cd web && npm install && npx nuxi generate
	rm -rf web/dist/*
	cp -r web/.output/public/* web/dist/

build: build-frontend
	go build -o bin/heya ./cmd/heya

build-go:
	go build -o bin/heya ./cmd/heya

run: build-go
	./bin/heya

dev-frontend:
	cd web && npm install && npx nuxi dev

test:
	go test ./...

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

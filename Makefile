.PHONY: build run test lint clean db-up db-down migrate

build:
	go build -o bin/kura ./cmd/kura

run: build
	./bin/kura

test:
	go test ./...

lint:
	go vet ./...

clean:
	rm -rf bin/

db-up:
	docker compose up -d postgres

db-down:
	docker compose down

migrate:
	goose -dir migrations postgres "$(DATABASE_URL)" up

migrate-down:
	goose -dir migrations postgres "$(DATABASE_URL)" down

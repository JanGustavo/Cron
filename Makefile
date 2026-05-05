.PHONY: dev/api dev/worker dev/scheduler migrate/up migrate/down sqlc/gen test lint build

# Rodar cada processo em dev
dev/api:
	go run ./cmd/api

dev/worker:
	go run ./cmd/worker

dev/scheduler:
	go run ./cmd/scheduler

# Migrations
migrate/up:
	migrate -path ./migrations -database "$(DATABASE_URL)" up

migrate/down:
	migrate -path ./migrations -database "$(DATABASE_URL)" down 1

# Gerar código a partir de queries SQL (requer sqlc instalado)
sqlc/gen:
	sqlc generate

# Testes
test:
	go test ./... -v -race

# Lint (requer golangci-lint)
lint:
	golangci-lint run ./...

# Build de todos os binários
build:
	go build -o bin/api ./cmd/api
	go build -o bin/worker ./cmd/worker
	go build -o bin/scheduler ./cmd/scheduler

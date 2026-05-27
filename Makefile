.PHONY: dev dev/api dev/worker dev/scheduler dev/front migrate/up migrate/down sqlc/gen test lint build

SHELL := /bin/bash

# Rodar todos os processos juntos com limpeza automática no Ctrl+C (kill 0)
dev:
	@echo "⚡ Iniciando todos os serviços do CronFlow (API, Scheduler, Worker, Frontend)..."
	@trap 'echo -e "\n🛑 Desligando serviços graciosamente..."; kill 0' INT; \
	go run ./cmd/api & \
	go run ./cmd/scheduler & \
	go run ./cmd/worker & \
	npm --prefix "../cron front" run dev & \
	wait

# Rodar cada processo em dev
dev/api:
	go run ./cmd/api

dev/worker:
	go run ./cmd/worker

dev/scheduler:
	go run ./cmd/scheduler

dev/front:
	npm --prefix "../cron front" run dev

# Migrations
migrate/up:
	docker run --rm --net=host -v $$(pwd)/migrations:/migrations migrate/migrate -path=/migrations -database "$(DATABASE_URL)" up

migrate/down:
	docker run --rm --net=host -v $$(pwd)/migrations:/migrations migrate/migrate -path=/migrations -database "$(DATABASE_URL)" down 1

# Gerar código a partir de queries SQL (requer sqlc instalado)
sqlc/gen:
	sqlc generate

# Testes
test:
	go test ./... -v -race

# Lint (requer golangci-lint)
lint:
	golangci-lint run ./...

VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "v1.0.0-dev")
BUILD_TIME := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS := -ldflags "-X github.com/JanGustavo/Cron/internal/api/handler.Version=$(VERSION) -X github.com/JanGustavo/Cron/internal/api/handler.BuildTime=$(BUILD_TIME)"

# Build de todos os binários
build:
	go build $(LDFLAGS) -o bin/api ./cmd/api
	go build $(LDFLAGS) -o bin/worker ./cmd/worker
	go build $(LDFLAGS) -o bin/scheduler ./cmd/scheduler

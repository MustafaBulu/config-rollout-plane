.PHONY: build test fmt vet dev-up dev-down migrate-up clean

COMPOSE_FILE := deploy/docker-compose/docker-compose.yml

build:
	go build ./cmd/...

test:
	go test ./...

fmt:
	go fmt ./...

vet:
	go vet ./...

dev-up:
	docker compose -f $(COMPOSE_FILE) up -d

dev-down:
	docker compose -f $(COMPOSE_FILE) down --remove-orphans

migrate-up:
	docker compose -f $(COMPOSE_FILE) exec -T postgres psql -U safe_config -d safe_config < migrations/000001_foundation.sql

clean:
	go clean

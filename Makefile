.PHONY: build test fmt vet config-validate simulate reliability dev-up dev-down migrate-up clean

COMPOSE_FILE := deploy/docker-compose/docker-compose.yml

build:
	go build ./cmd/...

test:
	go test ./...

fmt:
	go fmt ./...

vet:
	go vet ./...

config-validate:
	go run ./cmd/cfgctl validate examples/config-as-code

simulate:
	go run ./cmd/simulator -agents 1000 -concurrency 64

reliability:
	go run ./cmd/reliability -scenario all -concurrency 32

dev-up:
	docker compose -f $(COMPOSE_FILE) up -d

dev-down:
	docker compose -f $(COMPOSE_FILE) down --remove-orphans

migrate-up:
	docker compose -f $(COMPOSE_FILE) exec -T postgres sh -c 'for f in /migrations/*.sql; do psql -U safe_config -d safe_config -f "$$f"; done'

clean:
	go clean

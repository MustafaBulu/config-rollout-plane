# SafeConfig

A Go-based control plane for safely distributing runtime configuration using progressive delivery, health guardrails, and automatic rollback.

This repository is currently at Milestone 2: data-plane snapshot delivery and agent local caching.

## Commands

```bash
make build
make test
make dev-up
make dev-down
```

## Services

- `control-plane`: authoritative configuration registry API, health endpoint on `:8080`
- `data-plane`: read-optimized snapshot API, health endpoint on `:8081`
- `agent`: local sidecar-style API with last-known-good cache, health endpoint on `:8082`

## Milestone 1 API

```bash
curl -X POST localhost:8080/v1/tenants \
  -H "Content-Type: application/json" \
  -d '{"id":"payments","name":"Payments"}'

curl -X POST localhost:8080/v1/tenants/payments/configs \
  -H "Content-Type: application/json" \
  -d '{"key":"payment.authorization.timeout","schema":{"type":"integer","minimum":100,"maximum":10000},"default":2000}'

curl -X POST localhost:8080/v1/tenants/payments/configs/payment.authorization.timeout/versions \
  -H "Content-Type: application/json" \
  -d '{"value":1500,"created_by":"developer@example.com"}'

curl -X POST localhost:8080/v1/tenants/payments/configs/payment.authorization.timeout/environments/production/stable \
  -H "Content-Type: application/json" \
  -d '{"version_number":1}'
```

## Local Dependencies

`make dev-up` starts PostgreSQL through Docker Compose.

Default database settings:

```text
host: localhost
port: 5432
database: safe_config
user: safe_config
password: safe_config
```

Apply migrations after PostgreSQL is running:

```bash
make migrate-up
```

Run the control plane with PostgreSQL persistence:

```bash
DATABASE_URL='postgres://safe_config:safe_config@localhost:5432/safe_config?sslmode=disable' go run ./cmd/control-plane
```

If `DATABASE_URL` is not set, the control plane starts with an in-memory store. That mode is useful for tests and quick local API checks, but data is lost when the process exits.

Optional PostgreSQL integration tests:

```bash
SAFE_CONFIG_TEST_DATABASE_URL='postgres://safe_config:safe_config@localhost:5432/safe_config?sslmode=disable' go test ./internal/storage/postgres
```

## Milestone 2 Local Snapshot Flow

Register an agent with the control plane:

```bash
curl -X POST localhost:8080/v1/agents/register \
  -H "Content-Type: application/json" \
  -d '{"bootstrap_token":"dev-bootstrap-token","id":"agent-1","service":"payment-api","environment":"production","instance":"payment-api-1"}'
```

Start a data plane with one seeded snapshot:

```bash
DATA_PLANE_AGENT_ID=agent-1 \
DATA_PLANE_AGENT_TOKEN='<instance credential>' \
DATA_PLANE_CONFIG_KEY='payment.authorization.timeout' \
DATA_PLANE_CONFIG_VALUE='1500' \
go run ./cmd/data-plane
```

Start an agent that polls the data plane and serves local config:

```bash
DATA_PLANE_URL='http://localhost:8081' \
AGENT_ID=agent-1 \
AGENT_INSTANCE_CREDENTIAL='<instance credential>' \
go run ./cmd/agent
```

Read config through the local agent:

```bash
curl localhost:8082/v1/config/payment.authorization.timeout
```

Secrets are outside the scope of this platform and must be stored in a dedicated secrets-management system.

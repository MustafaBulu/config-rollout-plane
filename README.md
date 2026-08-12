# SafeConfig

A Go-based control plane for safely distributing runtime configuration using progressive delivery, health guardrails, and automatic rollback.

This repository is currently at Milestone 1: config registry.

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
- `agent`: local sidecar-style API placeholder, health endpoint on `:8082`

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

Secrets are outside the scope of this platform and must be stored in a dedicated secrets-management system.

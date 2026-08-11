# SafeConfig

A Go-based control plane for safely distributing runtime configuration using progressive delivery, health guardrails, and automatic rollback.

This repository is currently at Milestone 0: foundation.

## Commands

```bash
make build
make test
make dev-up
make dev-down
```

## Services

- `control-plane`: authoritative configuration and rollout API, health endpoint on `:8080`
- `data-plane`: read-optimized snapshot API, health endpoint on `:8081`
- `agent`: local sidecar-style API placeholder, health endpoint on `:8082`

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

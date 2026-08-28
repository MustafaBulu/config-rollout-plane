# SafeConfig

SafeConfig is a Go backend project for rolling out runtime configuration safely.

It models the split used in real internal platforms: a write-heavy control plane owns definitions,
versions, rollouts, agent identity, acknowledgements, and guardrails; a read-optimized data plane
serves snapshots to local agents; applications read from the local agent instead of calling the
control plane directly.

The interesting part is not "feature flags". The project focuses on the failure modes around
operational configuration:

- a bad value should be rejected before it becomes a version
- agents should receive the exact version intended for them
- percentage rollout cohorts should be deterministic and stable
- rollout promotion should wait for acknowledgement coverage
- health guardrails should stop or roll back unsafe changes
- applications should continue from a last-known-good local cache during backend outages

## Highlights

- Go 1.26, standard-library HTTP handlers, PostgreSQL, and structured JSON logs
- JSON Schema validation, immutable config versions, and stable environment pointers
- Agent registration, instance credentials, heartbeats, and acknowledgements
- Data-plane snapshots with ETag support and credential/path mismatch protection
- Local agent cache with checksum validation and atomic last-known-good writes
- Deterministic 5/25/100 rollouts with frozen cohorts and acknowledgement gates
- Prometheus guardrails, automatic rollback, and rollback verification
- Config-as-code, simulator, reliability harness, Docker, Kubernetes, and AWS/EKS scaffolding

## Architecture

```text
Config author / CLI
        |
        v
Control Plane  -> PostgreSQL
        |
        v
Data Plane
        |
        v
Local Agent  -> application process
```

Command entrypoints:

- `cmd/control-plane`: tenant, config, version, stable state, rollout, agent, heartbeat, and acknowledgement APIs
- `cmd/data-plane`: agent-specific snapshot API
- `cmd/agent`: local HTTP agent and snapshot cache
- `cmd/cfgctl`: config-as-code validation and apply CLI
- `cmd/simulator`: virtual-agent rollout simulator
- `cmd/reliability`: local failure-scenario harness

The Spring Boot payment demo service in `examples/demo-service` reads config from the local agent and
exports Prometheus metrics used by rollout guardrails.

Details: `docs/architecture.md`, `docs/rollout-model.md`, `docs/adr/0001-foundation-first.md`.

## Quick Start

This path runs the local agent against a seeded in-memory data plane. It is the fastest way to see
the snapshot and local config API working.

The command examples use Bash-style environment variables. In PowerShell, set variables with
`$env:NAME="value"` before running the matching `go run` command.

Terminal 1:

```bash
DATA_PLANE_AGENT_ID=agent-1 \
DATA_PLANE_AGENT_TOKEN=dev-agent-token \
DATA_PLANE_CONFIG_KEY=payment.failure_rate \
DATA_PLANE_CONFIG_VALUE=0 \
go run ./cmd/data-plane
```

Terminal 2:

```bash
DATA_PLANE_URL=http://localhost:8081 \
AGENT_ID=agent-1 \
AGENT_INSTANCE_CREDENTIAL=dev-agent-token \
go run ./cmd/agent
```

Read the config from the local agent:

```bash
curl http://localhost:8082/v1/config/payment.failure_rate
```

Expected shape:

```json
{
  "key": "payment.failure_rate",
  "version": 1,
  "value": 0
}
```

## Full Local Flow

For the full control-plane/data-plane path, start PostgreSQL first:

```bash
docker compose -f deploy/docker-compose/docker-compose.yml up -d
```

Apply migrations:

```bash
docker compose -f deploy/docker-compose/docker-compose.yml exec -T postgres sh -c 'for f in /migrations/*.sql; do psql -U safe_config -d safe_config -f "$f"; done'
```

Run the control plane:

```bash
DATABASE_URL='postgres://safe_config:safe_config@localhost:5432/safe_config?sslmode=disable' \
AGENT_BOOTSTRAP_TOKEN=dev-bootstrap-token \
go run ./cmd/control-plane
```

Run the data plane:

```bash
DATABASE_URL='postgres://safe_config:safe_config@localhost:5432/safe_config?sslmode=disable' \
DATA_PLANE_TENANTS=payments \
go run ./cmd/data-plane
```

Load the demo config-as-code manifests:

```bash
go run ./cmd/cfgctl apply \
  --control-plane-url http://localhost:8080 \
  examples/config-as-code
```

Run an agent that auto-registers with the control plane:

```bash
CONTROL_PLANE_URL=http://localhost:8080 \
DATA_PLANE_URL=http://localhost:8081 \
AGENT_BOOTSTRAP_TOKEN=dev-bootstrap-token \
AGENT_ID=payment-agent-1 \
AGENT_SERVICE=payment-service \
AGENT_ENVIRONMENT=production \
go run ./cmd/agent
```

Then read the active config through the agent:

```bash
curl http://localhost:8082/v1/config/payment.failure_rate
```

## Config-As-Code

Example manifests live in `examples/config-as-code`.

Validate them:

```bash
go run ./cmd/cfgctl validate examples/config-as-code
```

Preview an apply without writing:

```bash
go run ./cmd/cfgctl apply --dry-run examples/config-as-code
```

Apply definitions, versions, and stable pointers:

```bash
go run ./cmd/cfgctl apply \
  --control-plane-url http://localhost:8080 \
  examples/config-as-code
```

Rollout manifests are skipped by default during writes. Start them explicitly:

```bash
go run ./cmd/cfgctl apply \
  --control-plane-url http://localhost:8080 \
  --include-rollouts \
  examples/config-as-code
```

`SAFECONFIG_CONTROL_PLANE_URL` and `SAFECONFIG_TOKEN` can be used instead of the matching flags.

## Simulator

The simulator creates virtual agents, walks a 5/25/100 rollout, reads rollout-aware snapshots, sends
acknowledgements, and prints latency/throughput numbers.

```bash
go run ./cmd/simulator -agents 1000 -concurrency 64
```

JSON output is available when the result needs to be captured by another tool:

```bash
go run ./cmd/simulator -agents 1000 -concurrency 64 -format json
```

Details and a sample local run: `docs/scale-simulator.md`.

## Reliability

The reliability harness exercises the failure behavior that the project is built around.

```bash
go run ./cmd/reliability -scenario all -concurrency 32
```

Available scenarios:

- `control-plane-restart`
- `data-plane-outage`
- `concurrent-rollout-acknowledgements`
- `rollback-propagation-timing`

JSON output:

```bash
go run ./cmd/reliability -scenario all -concurrency 32 -format json
```

Details: `docs/reliability-scenarios.md`, `docs/reliability-results.md`,
`docs/failure-semantics.md`.

## APIs

OpenAPI documentation is in `api/openapi.yaml`.

Main control-plane groups:

- health: `GET /healthz`, `GET /livez`, `GET /readyz`
- tenants: `POST /v1/tenants`, `GET /v1/tenants`, `GET /v1/tenants/{tenant}`
- configs: create/list/get definitions, create/list versions, set/get stable versions
- agents: register, heartbeat, acknowledgements
- rollouts: create and inspect rollout state

Data-plane API:

- `GET /v1/agents/{agentID}/snapshot`

Local-agent API:

- `GET /v1/snapshot`
- `GET /v1/config/{key}`

Demo service API:

- `GET /v1/payments/authorize`
- `GET /actuator/health`
- `GET /actuator/prometheus`

## Configuration

Most local defaults are intentionally usable without extra files.

| Area | Main variables |
| --- | --- |
| Common | `DATABASE_URL`, `LOG_LEVEL=debug` |
| Control plane | `CONTROL_PLANE_ADDR`, `AGENT_BOOTSTRAP_TOKEN`, `AGENT_CREDENTIAL_TTL`, `PROMETHEUS_URL`, `ROLLOUT_RECONCILE_INTERVAL` |
| Data plane | `DATA_PLANE_ADDR`, `DATA_PLANE_TENANTS`, `DATA_PLANE_AGENT_ID`, `DATA_PLANE_AGENT_TOKEN`, `DATA_PLANE_CONFIG_KEY`, `DATA_PLANE_CONFIG_VALUE` |
| Agent | `AGENT_ADDR`, `DATA_PLANE_URL`, `CONTROL_PLANE_URL`, `AGENT_ID`, `AGENT_INSTANCE_CREDENTIAL`, `AGENT_BOOTSTRAP_TOKEN`, `AGENT_SERVICE`, `AGENT_ENVIRONMENT`, `AGENT_CACHE_PATH`, `AGENT_POLL_INTERVAL` |

Each HTTP service also supports read, write, idle, and shutdown timeout env vars using its own
prefix, such as `CONTROL_PLANE_READ_TIMEOUT` or `AGENT_SHUTDOWN_TIMEOUT`.

## Verification

Run the Go test suite:

```bash
go test ./...
```

Build all Go commands:

```bash
go build ./cmd/...
```

Run static checks:

```bash
go vet ./...
go run honnef.co/go/tools/cmd/staticcheck@v0.8.1 ./...
```

The same checks are available through Make:

```bash
make test
make build
make lint
make config-validate
```

Optional PostgreSQL integration test:

```bash
SAFE_CONFIG_TEST_DATABASE_URL='postgres://safe_config:safe_config@localhost:5432/safe_config?sslmode=disable' \
go test ./internal/storage/postgres
```

Demo service tests:

```bash
cd examples/demo-service
mvn test
```

## Kubernetes and AWS

Local Kubernetes manifests use Kustomize and are documented in `deploy/kubernetes/README.md`.

Render them locally:

```bash
kubectl kustomize deploy/kubernetes/base
kubectl kustomize deploy/kubernetes/demo
```

AWS Terraform lives in `deploy/terraform/aws` and prepares the showcase VPC, EKS cluster, node group,
security groups, IAM roles, and optional ECR repositories.

```bash
terraform -chdir=deploy/terraform/aws init
terraform -chdir=deploy/terraform/aws validate
terraform -chdir=deploy/terraform/aws plan
```

AWS/EKS Kubernetes overlays live in `deploy/kubernetes/aws`.

```bash
make aws-platform-render
make aws-demo-render
```

Do not run `terraform apply` unless you are ready to create billable AWS resources.

## Security Notes

- SafeConfig is not a secret manager.
- Do not store passwords, API keys, private keys, or tokens as configuration values.
- Development bootstrap tokens are for local use only.
- Instance credentials are bound to one agent identity.
- Snapshot requests reject credential/path mismatches with `403 Forbidden`.
- Token rotation, authorization policy, and persistent audit hardening are intentionally left as later production work.

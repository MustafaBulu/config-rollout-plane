# SafeConfig

A backend architecture showcase project for safe runtime configuration delivery.

SafeConfig demonstrates a production-style control-plane/data-plane architecture for distributing
configuration changes to running services without redeploying application binaries. The project is
written in Go and focuses on the safety mechanics around configuration definitions, immutable
versions, agent identity, snapshot delivery, acknowledgements, and last-known-good local caching.

The design goal is not to build a feature flag SaaS clone. The core problem is safer operational
configuration delivery: services should be able to receive runtime config updates, validate them,
acknowledge the exact version they received, and continue operating from a local cache during backend
outages.

Percentage rollout stages, deterministic agent assignment, frozen target cohorts, acknowledgement
coverage, deployment-timeout rollback, Prometheus health guardrails, automatic health-based rollback,
and rollback verification are implemented.

## Architecture

The platform is organized as a Go monorepo with four runnable binaries.

- `control-plane`: authoritative API for tenants, config definitions, immutable versions, stable environment state, agent registration, heartbeats, and acknowledgements.
- `data-plane`: read-optimized API that serves agent-specific configuration snapshots with ETag support.
- `agent`: local sidecar-style process that polls snapshots, validates checksums, writes a durable cache, and exposes config to the application over localhost.
- `simulator`: local virtual-agent load simulator for scale evidence.

```text
Developer / API client
        |
        v
Control Plane  -> PostgreSQL
        |
        v
Data Plane
        |
        v
Local Agent  -> application service
```

The control-plane registry can run against PostgreSQL for durable state. Tests and quick local runs can
use the in-memory store behind the same Go interface.

## Configuration Delivery Flow

1. A client creates a tenant.
2. A client creates a configuration definition with a JSON Schema.
3. Every new config value is validated against the schema.
4. A valid value creates an immutable version.
5. A version can be marked stable for an environment.
6. An agent registers with a bootstrap token and receives an instance credential.
7. The data plane serves an agent-specific snapshot.
8. The local agent validates the snapshot checksum and writes it to disk atomically.
9. The application reads config from the local agent.
10. If the backend is unavailable later, the local agent keeps serving the last-known-good snapshot.

## Implemented Capabilities

- Go 1.26 monorepo
- HTTP/JSON APIs using the standard library
- Structured JSON logging with `slog`
- Graceful shutdown for all binaries
- Tenant registry
- Configuration definitions
- JSON Schema validation
- Immutable configuration versions
- Stable version pointer per config/environment
- PostgreSQL schema migrations
- PostgreSQL-backed config registry store
- In-memory stores for tests and local experiments
- Agent registration with bootstrap credential exchange
- Instance credentials bound to one agent identity
- Agent heartbeat endpoint
- Agent acknowledgement endpoint
- Percentage rollout creation and inspection endpoints
- Default 5/25/100 rollout stages
- Deterministic rollout bucketing
- Frozen rollout target cohorts
- Acknowledgement coverage based promotion
- Deployment-timeout rollback
- Prometheus guardrail evaluation
- Automatic health-based rollback
- Rollback verification with VERIFIED/PARTIAL outcomes
- 1000-agent virtual simulator
- Simulator latency and throughput reporting
- Spring Boot payment demo service
- Demo service Prometheus metrics
- Docker build files for local images
- kind Kubernetes manifests for platform and demo workloads
- Agent sidecar auto-registration for Kubernetes demos
- Data-plane snapshot endpoint
- Rollout-aware data-plane snapshots when PostgreSQL is configured
- ETag and `If-None-Match` support
- Credential/path mismatch protection with `403 Forbidden`
- Agent snapshot polling client
- Checksum validation before cache writes
- Atomic local snapshot cache writes
- Local agent config API
- Unit and handler tests for core flows
- Docker Compose PostgreSQL development environment
- GitHub Actions CI

## Components

### `cmd/control-plane`

Runs the control-plane HTTP API on port `8080` by default.

Main responsibilities:

- config registry writes
- schema validation
- stable version state
- agent registration
- heartbeat tracking
- acknowledgement recording

### `cmd/data-plane`

Runs the data-plane HTTP API on port `8081` by default.

Main responsibilities:

- serve agent snapshots
- enforce credential subject matching
- return `304 Not Modified` when the agent already has the latest snapshot

### `cmd/agent`

Runs the local agent HTTP API on port `8082` by default.

Main responsibilities:

- poll the data plane
- validate snapshot checksums
- write and load local snapshot cache
- serve config to local applications

### `cmd/simulator`

Runs a local in-memory 5/25/100 rollout simulation with virtual agents.

Main responsibilities:

- register virtual agents
- read rollout-aware snapshots
- acknowledge assigned rollout stages
- report snapshot and acknowledgement latency

## Local Runtime

Start PostgreSQL:

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
AGENT_BOOTSTRAP_TOKEN='dev-bootstrap-token' \
go run ./cmd/control-plane
```

Run the data plane with a seeded demo snapshot:

```bash
DATA_PLANE_AGENT_ID='agent-1' \
DATA_PLANE_AGENT_TOKEN='<instance credential>' \
DATA_PLANE_CONFIG_KEY='payment.authorization.timeout' \
DATA_PLANE_CONFIG_VALUE='1500' \
go run ./cmd/data-plane
```

Run the local agent:

```bash
DATA_PLANE_URL='http://localhost:8081' \
AGENT_ID='agent-1' \
AGENT_INSTANCE_CREDENTIAL='<instance credential>' \
go run ./cmd/agent
```

## Example API Flow

### 1. Create tenant

```bash
curl -X POST localhost:8080/v1/tenants \
  -H "Content-Type: application/json" \
  -d '{"id":"payments","name":"Payments"}'
```

### 2. Create config definition

```bash
curl -X POST localhost:8080/v1/tenants/payments/configs \
  -H "Content-Type: application/json" \
  -d '{"key":"payment.authorization.timeout","schema":{"type":"integer","minimum":100,"maximum":10000},"default":2000}'
```

### 3. Create immutable version

```bash
curl -X POST localhost:8080/v1/tenants/payments/configs/payment.authorization.timeout/versions \
  -H "Content-Type: application/json" \
  -d '{"value":1500,"created_by":"developer@example.com"}'
```

### 4. Mark version stable

```bash
curl -X POST localhost:8080/v1/tenants/payments/configs/payment.authorization.timeout/environments/production/stable \
  -H "Content-Type: application/json" \
  -d '{"version_number":1}'
```

### 5. Register agent

```bash
curl -X POST localhost:8080/v1/agents/register \
  -H "Content-Type: application/json" \
  -d '{"bootstrap_token":"dev-bootstrap-token","id":"agent-1","service":"payment-api","environment":"production","instance":"payment-api-1"}'
```

Copy the returned `instance_credential` and use it for data-plane and local-agent requests.

### 6. Read config from local agent

```bash
curl localhost:8082/v1/config/payment.authorization.timeout
```

Example response:

```json
{
  "key": "payment.authorization.timeout",
  "version": 1,
  "value": 1500
}
```

## Service URLs

- Control plane: `http://localhost:8080`
- Data plane: `http://localhost:8081`
- Local agent: `http://localhost:8082`

## APIs

Control plane:

- `POST /v1/tenants`
- `GET /v1/tenants`
- `GET /v1/tenants/{tenant}`
- `POST /v1/tenants/{tenant}/configs`
- `GET /v1/tenants/{tenant}/configs`
- `GET /v1/tenants/{tenant}/configs/{key}`
- `POST /v1/tenants/{tenant}/configs/{key}/versions`
- `GET /v1/tenants/{tenant}/configs/{key}/versions`
- `POST /v1/tenants/{tenant}/configs/{key}/environments/{environment}/stable`
- `GET /v1/tenants/{tenant}/configs/{key}/environments/{environment}/stable`
- `POST /v1/agents/register`
- `POST /v1/agents/{agentID}/heartbeat`
- `POST /v1/agents/{agentID}/acknowledgements`
- `POST /v1/rollouts`
- `GET /v1/rollouts/{rolloutID}`

Data plane:

- `GET /v1/agents/{agentID}/snapshot`

Local agent:

- `GET /healthz`
- `GET /readyz`
- `GET /v1/snapshot`
- `GET /v1/config/{key}`

Demo service:

- `GET /v1/payments/authorize`
- `GET /actuator/health`
- `GET /actuator/prometheus`

OpenAPI documentation is available in `api/openapi.yaml`.

## Configuration

Control plane:

- `CONTROL_PLANE_ADDR` default: `:8080`
- `DATABASE_URL`
- `AGENT_BOOTSTRAP_TOKEN` default: `dev-bootstrap-token`
- `AGENT_CREDENTIAL_TTL` default: `15m`
- `PROMETHEUS_URL` optional Prometheus base URL for rollout guardrails
- `PROMETHEUS_QUERY_TIMEOUT` default: `2s`
- `ROLLOUT_RECONCILE_INTERVAL` default: `2s`

Data plane:

- `DATA_PLANE_ADDR` default: `:8081`
- `DATABASE_URL`
- `DATA_PLANE_TENANTS` optional comma-separated tenant filter for dynamic snapshots
- `DATA_PLANE_TENANT_ID` optional single-tenant filter for dynamic snapshots
- `DATA_PLANE_AGENT_ID`
- `DATA_PLANE_AGENT_TOKEN`
- `DATA_PLANE_CONFIG_KEY`
- `DATA_PLANE_CONFIG_VALUE`

Agent:

- `AGENT_ADDR` default: `:8082`
- `DATA_PLANE_URL`
- `AGENT_ID`
- `AGENT_INSTANCE_CREDENTIAL`
- `CONTROL_PLANE_URL` optional, enables agent auto-registration and acknowledgements
- `AGENT_BOOTSTRAP_TOKEN` required for auto-registration
- `AGENT_SERVICE` or `SERVICE_NAME` default: `payment-service`
- `AGENT_ENVIRONMENT` or `ENVIRONMENT` default: `production`
- `AGENT_INSTANCE` default: `AGENT_ID`
- `AGENT_CACHE_PATH` default: `var/safeconfig/snapshot.json`
- `AGENT_POLL_INTERVAL` default: `2s`

Demo service:

- `SERVER_PORT` default: `8090`
- `SAFECONFIG_AGENT_URL` default: `http://localhost:8082`
- `SAFECONFIG_FAILURE_RATE_KEY` default: `payment.failure_rate`
- `DEMO_DEFAULT_FAILURE_RATE` default: `0.0`
- `SAFECONFIG_REQUEST_TIMEOUT` default: `500ms`

## Test

Run all tests:

```bash
go test ./...
```

Build all binaries:

```bash
go build ./cmd/...
```

Run static checks:

```bash
go vet ./...
```

Optional PostgreSQL integration test:

```bash
SAFE_CONFIG_TEST_DATABASE_URL='postgres://safe_config:safe_config@localhost:5432/safe_config?sslmode=disable' \
go test ./internal/storage/postgres
```

Demo service test:

```bash
cd examples/demo-service
mvn test
```

Kubernetes manifest validation:

```bash
kubectl kustomize deploy/kubernetes/base
kubectl kustomize deploy/kubernetes/demo
```

The local Kubernetes demo is documented in `deploy/kubernetes/README.md`.
The recorded Kubernetes demo scenario is documented in `docs/kubernetes-demo-scenario.md`.
The reliability scenario harness is documented in `docs/reliability-scenarios.md`.
The latest local reliability evidence is documented in `docs/reliability-results.md`.

## Scale Simulator

Run the default 1000-agent local simulation:

```bash
go run ./cmd/simulator -agents 1000 -concurrency 64
```

The simulator is documented in `docs/scale-simulator.md`.

## Reliability Evidence

Run the local reliability evidence suite:

```bash
make reliability
```

Equivalent direct command:

```bash
go run ./cmd/reliability -scenario all -concurrency 32
```

The latest recorded local result is documented in `docs/reliability-results.md`.

## AWS Terraform

The AWS showcase Terraform root module is in `deploy/terraform/aws`.

It prepares the VPC, subnets, routing, security groups, base IAM roles, EKS cluster, and default
managed node group.

```bash
cd deploy/terraform/aws
terraform init
terraform validate
terraform plan
```

After `terraform apply`, configure and validate `kubectl`:

```bash
make eks-kubeconfig
make eks-nodes
```

Do not run `terraform apply` unless you are ready to create billable AWS resources. Destroy the
showcase stack after testing.

## Config-as-Code

SafeConfig YAML manifests use `apiVersion`, `kind`, `metadata`, and `spec` fields. The workflow
validates manifests in CI and can apply reviewed manifests to the control plane.

Example manifests live in `examples/config-as-code`.

Validate them locally:

```bash
go run ./cmd/cfgctl validate examples/config-as-code
```

The same validation runs in GitHub Actions through:

```bash
make config-validate
```

Preview the apply plan without writing:

```bash
go run ./cmd/cfgctl apply --dry-run examples/config-as-code
```

Apply reviewed manifests to a running control plane:

```bash
go run ./cmd/cfgctl apply \
  --control-plane-url http://localhost:8080 \
  examples/config-as-code
```

Rollout manifests are skipped by default during writes. Start rollout manifests explicitly:

```bash
go run ./cmd/cfgctl apply \
  --control-plane-url http://localhost:8080 \
  --include-rollouts \
  examples/config-as-code
```

`SAFECONFIG_CONTROL_PLANE_URL` and `SAFECONFIG_TOKEN` can be used instead of the corresponding flags.

## Security Notes

- SafeConfig is not a secret manager.
- Do not store passwords, API keys, private keys, or tokens as configuration values.
- Development bootstrap tokens are for local use only.
- Instance credentials are bound to one agent identity.
- Snapshot requests reject credential/path mismatches with `403 Forbidden`.
- Token rotation, authorization policy, and persistent audit hardening should be completed before production use.

## Planned Work

- Multi-replica control plane behavior

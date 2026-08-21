# SafeConfig Demo Service

This Spring Boot service simulates payment authorization traffic for the rollout demo.

It reads `payment.failure_rate` from the local SafeConfig agent:

- `0.0` means all demo requests succeed.
- `0.2` means roughly 20 percent of deterministic order IDs fail.

If the local agent is unavailable, the service keeps running with `DEMO_DEFAULT_FAILURE_RATE`, which defaults to `0.0`.

## Run Locally

From this directory:

```bash
mvn test
mvn spring-boot:run
```

The service listens on `http://localhost:8090` by default.

Useful endpoints:

```bash
curl 'http://localhost:8090/v1/payments/authorize?orderId=order-1'
curl 'http://localhost:8090/actuator/health'
curl 'http://localhost:8090/actuator/prometheus'
```

## Local SafeConfig Flow

Start PostgreSQL and apply migrations from the repository root:

```bash
make dev-up
make migrate-up
```

Run the control plane:

```bash
DATABASE_URL='postgres://safe_config:safe_config@localhost:5432/safe_config?sslmode=disable' \
AGENT_BOOTSTRAP_TOKEN='dev-bootstrap-token' \
go run ./cmd/control-plane
```

Create the demo config:

```bash
curl -X POST localhost:8080/v1/tenants \
  -H 'Content-Type: application/json' \
  -d '{"id":"payments","name":"Payments"}'

curl -X POST localhost:8080/v1/tenants/payments/configs \
  -H 'Content-Type: application/json' \
  -d @examples/configs/payment-failure-rate-definition.json

curl -X POST localhost:8080/v1/tenants/payments/configs/payment.failure_rate/versions \
  -H 'Content-Type: application/json' \
  -d @examples/configs/payment-failure-rate-v1.json

curl -X POST localhost:8080/v1/tenants/payments/configs/payment.failure_rate/environments/production/stable \
  -H 'Content-Type: application/json' \
  -d '{"version_number":1}'

curl -X POST localhost:8080/v1/tenants/payments/configs/payment.failure_rate/versions \
  -H 'Content-Type: application/json' \
  -d @examples/configs/payment-failure-rate-v2-bad.json
```

Register an agent and keep the returned `instance_credential`:

```bash
curl -X POST localhost:8080/v1/agents/register \
  -H 'Content-Type: application/json' \
  -d '{"bootstrap_token":"dev-bootstrap-token","id":"payment-demo-agent-1","service":"payment-service","environment":"production","instance":"payment-demo-1"}'
```

Run the data plane from the repository root:

```bash
DATABASE_URL='postgres://safe_config:safe_config@localhost:5432/safe_config?sslmode=disable' \
DATA_PLANE_TENANTS='payments' \
go run ./cmd/data-plane
```

Run the local agent from the repository root:

```bash
DATA_PLANE_URL='http://localhost:8081' \
AGENT_ID='payment-demo-agent-1' \
AGENT_INSTANCE_CREDENTIAL='<instance credential>' \
go run ./cmd/agent
```

Run this demo service:

```bash
SAFECONFIG_AGENT_URL='http://localhost:8082' \
mvn spring-boot:run
```

Generate traffic:

```bash
for i in $(seq 1 100); do curl -s "http://localhost:8090/v1/payments/authorize?orderId=order-$i" > /dev/null; done
```

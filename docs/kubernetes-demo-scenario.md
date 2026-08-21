# Kubernetes Demo Scenario

This is the recorded demo scenario for the local Kubernetes environment. It shows a bad runtime
configuration rollout reaching a small canary cohort, failing a Prometheus guardrail, and rolling
back to the previous stable version.

## Goal

Demonstrate these capabilities in one repeatable flow:

- Spring Boot payment demo service
- Prometheus metrics from the demo service
- local `kind` Kubernetes environment
- multiple application pods
- Go SafeConfig agent sidecars
- health-based rollback during a bad rollout

## Setup

Prerequisites:

- Docker Desktop
- `kind`
- `kubectl`

From the repository root, build the local images:

```bash
docker build -f deploy/docker/safeconfig.Dockerfile --build-arg APP=control-plane -t safeconfig/control-plane:local .
docker build -f deploy/docker/safeconfig.Dockerfile --build-arg APP=data-plane -t safeconfig/data-plane:local .
docker build -f deploy/docker/safeconfig.Dockerfile --build-arg APP=agent -t safeconfig/agent:local .
docker build -t safeconfig/payment-demo-service:local examples/demo-service
```

Create the cluster and load the images:

```bash
kind create cluster --config deploy/kubernetes/kind-cluster.yaml
kind load docker-image safeconfig/control-plane:local --name safeconfig-demo
kind load docker-image safeconfig/data-plane:local --name safeconfig-demo
kind load docker-image safeconfig/agent:local --name safeconfig-demo
kind load docker-image safeconfig/payment-demo-service:local --name safeconfig-demo
```

Deploy the platform:

```bash
kubectl apply -k deploy/kubernetes/base
kubectl -n safe-config-system wait --for=condition=available deploy/postgres --timeout=120s
kubectl -n safe-config-system wait --for=condition=complete job/safeconfig-migrations --timeout=120s
kubectl -n safe-config-system wait --for=condition=available deploy/control-plane --timeout=120s
kubectl -n safe-config-system wait --for=condition=available deploy/data-plane --timeout=120s
kubectl -n safe-config-system wait --for=condition=available deploy/prometheus --timeout=120s
```

In a separate shell, keep the control plane port-forward open:

```bash
kubectl -n safe-config-system port-forward svc/control-plane 8080:8080
```

Seed the stable config and the bad candidate:

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

Deploy the demo workload:

```bash
kubectl apply -k deploy/kubernetes/demo
kubectl -n demo rollout status deploy/payment-demo-service --timeout=180s
kubectl -n demo get pods
```

Expected checkpoint: 20 `payment-demo-service` pods are running, each with an `app` container and a
`safeconfig-agent` sidecar container.

## Recording Flow

Start traffic in the cluster:

```bash
kubectl -n demo run traffic --rm -i --restart=Never --image=curlimages/curl:8.15.0 -- \
  sh -c 'while true; do for i in $(seq 1 200); do curl -s "http://payment-demo-service:8090/v1/payments/authorize?orderId=order-$i" > /dev/null; done; sleep 1; done'
```

While traffic is running, show the baseline metrics target:

```bash
kubectl -n safe-config-system port-forward svc/prometheus 9090:9090
```

In another shell:

```bash
curl -g 'localhost:9090/api/v1/query?query=sum(rate(payment_requests_total[30s]))'
```

Start the bad rollout:

```bash
curl -X POST localhost:8080/v1/rollouts \
  -H 'Content-Type: application/json' \
  -d @examples/configs/payment-failure-rate-rollout-bad.json
```

Copy the returned rollout id and watch it:

```bash
curl localhost:8080/v1/rollouts/<rollout_id>
```

Useful supporting views during the recording:

```bash
kubectl -n demo logs deploy/payment-demo-service -c safeconfig-agent --tail=40
kubectl -n demo logs deploy/payment-demo-service -c app --tail=40
curl -g 'localhost:9090/api/v1/query?query=sum(rate(payment_requests_total{config_version="2",result="error"}[30s]))'
```

## Acceptance Evidence

The demo is complete when these checkpoints are visible:

1. The demo deployment has 20 ready pods.
2. SafeConfig agent sidecars register with the control plane and poll the data plane.
3. The bad rollout assigns config version 2 to the frozen 5 percent cohort.
4. The Spring Boot demo service emits `payment_requests_total` with `config_version` and `result` labels.
5. The Prometheus guardrail evaluates the candidate error rate as unhealthy.
6. The rollout enters `ROLLING_BACK`.
7. Candidate delivery stops.
8. The rollout finishes as `ROLLED_BACK` with `rollback_status` set to `VERIFIED` or `PARTIAL`.

## Cleanup

```bash
kind delete cluster --name safeconfig-demo
```

# Kubernetes Demo

This directory contains a local `kind` demo for SafeConfig.

It runs:

- SafeConfig control plane
- SafeConfig data plane
- PostgreSQL
- Prometheus
- 20 `payment-demo-service` pods, each with a SafeConfig agent sidecar

## Prerequisites

- Docker Desktop
- `kind`
- `kubectl`

Docker Desktop must be running before creating the cluster.

## Build Images

```bash
docker build -f deploy/docker/safeconfig.Dockerfile --build-arg APP=control-plane -t safeconfig/control-plane:local .
docker build -f deploy/docker/safeconfig.Dockerfile --build-arg APP=data-plane -t safeconfig/data-plane:local .
docker build -f deploy/docker/safeconfig.Dockerfile --build-arg APP=agent -t safeconfig/agent:local .
docker build -t safeconfig/payment-demo-service:local examples/demo-service
```

## Start kind

```bash
kind create cluster --config deploy/kubernetes/kind-cluster.yaml

kind load docker-image safeconfig/control-plane:local --name safeconfig-demo
kind load docker-image safeconfig/data-plane:local --name safeconfig-demo
kind load docker-image safeconfig/agent:local --name safeconfig-demo
kind load docker-image safeconfig/payment-demo-service:local --name safeconfig-demo
```

## Deploy Platform

```bash
kubectl apply -k deploy/kubernetes/base
kubectl -n safe-config-system wait --for=condition=available deploy/postgres --timeout=120s
kubectl -n safe-config-system wait --for=condition=complete job/safeconfig-migrations --timeout=120s
kubectl -n safe-config-system wait --for=condition=available deploy/control-plane --timeout=120s
kubectl -n safe-config-system wait --for=condition=available deploy/data-plane --timeout=120s
kubectl -n safe-config-system wait --for=condition=available deploy/prometheus --timeout=120s
```

## Seed Config

Port-forward the control plane:

```bash
kubectl -n safe-config-system port-forward svc/control-plane 8080:8080
```

In another shell:

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

## Deploy Demo Workload

```bash
kubectl apply -k deploy/kubernetes/demo
kubectl -n demo rollout status deploy/payment-demo-service --timeout=180s
```

The SafeConfig agent sidecars auto-register with the control plane using the development bootstrap token.

## Generate Traffic

```bash
kubectl -n demo run traffic --rm -i --restart=Never --image=curlimages/curl:8.15.0 -- \
  sh -c 'while true; do for i in $(seq 1 200); do curl -s "http://payment-demo-service:8090/v1/payments/authorize?orderId=order-$i" > /dev/null; done; sleep 1; done'
```

## Start Bad Rollout

```bash
curl -X POST localhost:8080/v1/rollouts \
  -H 'Content-Type: application/json' \
  -d @examples/configs/payment-failure-rate-rollout-bad.json
```

Watch the rollout:

```bash
curl localhost:8080/v1/rollouts/<rollout_id>
```

Expected behavior:

1. A frozen 5 percent cohort receives config version 2.
2. The demo service emits `payment_requests_total{config_version="2",result="error"}`.
3. Prometheus guardrail evaluates the candidate error rate.
4. The rollout moves to `ROLLING_BACK`.
5. Candidate delivery stops and agents acknowledge the previous stable version.
6. Rollback finishes with `rollback_status` of `VERIFIED` or `PARTIAL`.

## Cleanup

```bash
kind delete cluster --name safeconfig-demo
```

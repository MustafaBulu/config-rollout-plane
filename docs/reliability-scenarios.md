# Reliability Scenario Harness

The reliability harness runs SafeConfig control-plane, data-plane and local-agent flows with
in-memory services and `httptest` servers. It models focused failure scenarios without requiring a
Kubernetes cluster or external cloud infrastructure.

Implemented scenarios:

- `control-plane-restart`: registers an agent, restarts the control-plane HTTP surface over the same
  backing services, then verifies the existing agent credential can heartbeat through the restarted
  control plane.
- `data-plane-outage`: registers an agent, warms the local snapshot cache from the data plane,
  injects a data-plane outage, verifies polling fails, and verifies the cached config remains
  readable.
- `concurrent-rollout-acknowledgements`: registers 200 agents, creates a 100 percent rollout, and
  acknowledges every rollout target concurrently through the HTTP control-plane API.
- `rollback-propagation-timing`: triggers an unhealthy guarded rollout, activates rollback
  verification, acknowledges stable rollback snapshots, and records propagation timing.

The harness exposes reusable failure hooks:

- `InjectFailure(FailureControlPlane)`
- `InjectFailure(FailureDataPlane)`
- `Recover(...)`
- `Restart(...)`

Run the scenarios with:

```bash
go test ./internal/reliability
```

Run the full evidence suite with:

```bash
go run ./cmd/reliability -scenario all -concurrency 32
```

Equivalent Make target:

```bash
make reliability
```

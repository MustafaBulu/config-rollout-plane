# Reliability Scenario Harness

The reliability harness runs SafeConfig control-plane, data-plane and local-agent flows with
in-memory services and `httptest` servers. It is intended to model focused failure scenarios before
publishing broader evidence in the next milestone increment.

Implemented scenarios:

- `control-plane-restart`: registers an agent, restarts the control-plane HTTP surface over the same
  backing services, then verifies the existing agent credential can heartbeat through the restarted
  control plane.
- `data-plane-outage`: registers an agent, warms the local snapshot cache from the data plane,
  injects a data-plane outage, verifies polling fails, and verifies the cached config remains
  readable.

The harness exposes reusable failure hooks:

- `InjectFailure(FailureControlPlane)`
- `InjectFailure(FailureDataPlane)`
- `Recover(...)`
- `Restart(...)`

Run the scenarios with:

```bash
go test ./internal/reliability
```

These scenarios are not the final evidence suite. They provide the shared runner, result structs and
failure hooks that the scale and reliability evidence suite can build on.

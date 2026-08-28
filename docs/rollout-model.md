# Rollout Model

Percentage rollout behavior is implemented for the local control-plane/data-plane flow.

The rollout engine uses explicit states, deterministic agent assignment, frozen stage target cohorts, acknowledgement coverage, Prometheus guardrail evaluation, promotion, deployment-timeout rollback, health-based rollback, and rollback verification.

Default stages are:

- 5%
- 25%
- 100%

At stage activation the control plane persists target rows for agents that match the rollout environment/service target and fall inside the deterministic bucket threshold. Agents that join after activation remain on the stable version until the next stage snapshot is activated.

When a stage has reached acknowledgement coverage and its minimum duration, configured guardrails are evaluated before promotion. A `HEALTHY` result allows promotion, `UNHEALTHY` starts rollback, and `UNKNOWN` pauses promotion. Unknown telemetry is not treated as success; if the rollout maximum duration expires while telemetry remains unknown, the rollout rolls back.

Rollback immediately stops serving the candidate version. The control plane then creates a rollback verification target set from the candidate stage cohort, asks those agents to acknowledge the previous stable version, and marks rollback as `VERIFIED` when coverage is reached or `PARTIAL` when the rollback timeout expires first.

## Reconciler Scope

The current rollout reconciler assumes a single active reconciliation loop. Multiple control-plane
HTTP replicas can serve API traffic, but enabling reconciliation in more than one replica should use
leader election or DB-backed locking.

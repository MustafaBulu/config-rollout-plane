# Rollout Model

Percentage rollout behavior is implemented for the local control-plane/data-plane flow.

The rollout engine uses explicit states, deterministic agent assignment, frozen stage target cohorts, acknowledgement coverage, promotion, and deployment-timeout rollback.

Default stages are:

- 5%
- 25%
- 100%

At stage activation the control plane persists target rows for agents that match the rollout environment/service target and fall inside the deterministic bucket threshold. Agents that join after activation remain on the stable version until the next stage snapshot is activated.

Prometheus guardrail evaluation and automatic health-based rollback are Milestone 4 work.

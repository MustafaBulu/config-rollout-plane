# Failure Semantics

The current services provide graceful shutdown and health/readiness endpoints.

Control-plane, data-plane, PostgreSQL, Prometheus, and agent cache failure behavior should be documented through executable tests as the rollout engine grows.

Prometheus guardrail failures are fail-safe. A query error, empty result, multiple-series result where a scalar is expected, timeout, or non-finite sample produces `UNKNOWN`; unknown guardrails pause promotion and never count as healthy. If a rollout remains unable to prove health until its maximum duration expires, the control plane starts rollback.

Rollback stops candidate delivery immediately. Candidate-stage agents are then asked to acknowledge the previous stable version through a rollback verification stage. The rollback is marked `VERIFIED` when acknowledgement coverage is reached and `PARTIAL` when the rollback timeout expires first.

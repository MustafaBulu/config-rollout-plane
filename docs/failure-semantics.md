# Failure Semantics

The current services provide graceful shutdown and health/readiness endpoints.

Control-plane, data-plane, PostgreSQL, Prometheus, and agent cache failure behavior should be documented through executable tests as the rollout engine grows.

# Architecture

SafeConfig is split into a control plane, data plane, and local agent.

Milestone 0 only establishes process boundaries, health endpoints, local PostgreSQL, and development commands. Authoritative rollout state, agent synchronization, and guardrail evaluation are implemented in later milestones.

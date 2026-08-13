# ADR 0001: Build the Foundation First

## Status

Accepted

## Context

The platform's main risk is safe distributed configuration rollout. The project needs a reliable service foundation before rollout logic.

## Decision

Start with a small Go monorepo skeleton, health endpoints, structured logging, graceful shutdown, Docker Compose for PostgreSQL, a migration entry point, and CI.

## Consequences

Rollout state machines and Prometheus guardrails are deferred until the supporting registry, data-plane, and agent behavior are in place.

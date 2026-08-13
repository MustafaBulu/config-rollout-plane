# Architecture

SafeConfig is split into a control plane, data plane, and local agent.

Milestone 1 adds the configuration registry inside the control plane:

- tenant records
- configuration definitions
- JSON Schema validation
- immutable configuration versions
- stable version pointers per environment

The control plane can run with two store implementations:

- `MemoryStore` for fast tests and quick local checks
- `PostgresStore` for durable local or production-style runs when `DATABASE_URL` is set

Both implementations satisfy the same `configregistry.Store` interface, so the service layer does not depend on PostgreSQL details.

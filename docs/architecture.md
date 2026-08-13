# Architecture

SafeConfig is split into a control plane, data plane, and local agent.

The control plane includes a configuration registry:

- tenant records
- configuration definitions
- JSON Schema validation
- immutable configuration versions
- stable version pointers per environment

The control plane can run with two store implementations:

- `MemoryStore` for fast tests and quick local checks
- `PostgresStore` for durable local or production-style runs when `DATABASE_URL` is set

Both implementations satisfy the same `configregistry.Store` interface, so the service layer does not depend on PostgreSQL details.

The data plane exposes an agent-specific read model. The snapshot endpoint is:

```text
GET /v1/agents/{agentID}/snapshot
```

It verifies that the bearer credential subject matches the path agent ID and supports `ETag` / `If-None-Match`.

Agents register through the control plane with a bootstrap token, receive an instance credential, poll snapshots, validate checksums, write a durable local snapshot, and expose:

```text
GET /v1/snapshot
GET /v1/config/{key}
```

If the backend is unavailable after a valid cache exists, the local agent API continues serving the last-known-good snapshot.

# Architecture

SafeConfig is split into a control plane, data plane, and local agent.

Milestone 1 adds the configuration registry inside the control plane:

- tenant records
- configuration definitions
- JSON Schema validation
- immutable configuration versions
- stable version pointers per environment

The current runtime store is in-memory so the API and domain rules can be exercised before PostgreSQL repository code is introduced. PostgreSQL schema migrations are present as the persistence contract for the next storage step.

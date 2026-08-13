CREATE TABLE IF NOT EXISTS agents (
    id TEXT PRIMARY KEY,
    service TEXT NOT NULL,
    environment TEXT NOT NULL CHECK (environment IN ('development', 'staging', 'production')),
    zone TEXT NOT NULL DEFAULT '',
    instance TEXT NOT NULL,
    labels JSONB NOT NULL DEFAULT '{}',
    registered_at TIMESTAMPTZ NOT NULL,
    last_seen_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS agent_credentials (
    token TEXT PRIMARY KEY,
    agent_id TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS agent_acknowledgements (
    id TEXT PRIMARY KEY,
    agent_id TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    config_definition_id TEXT NOT NULL REFERENCES config_definitions(id) ON DELETE RESTRICT,
    version_id TEXT NOT NULL REFERENCES config_versions(id) ON DELETE RESTRICT,
    snapshot_revision BIGINT NOT NULL CHECK (snapshot_revision > 0),
    counted BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL,
    UNIQUE (agent_id, config_definition_id, version_id, snapshot_revision)
);

INSERT INTO schema_migrations (version, name)
VALUES (3, 'agents')
ON CONFLICT (version) DO NOTHING;

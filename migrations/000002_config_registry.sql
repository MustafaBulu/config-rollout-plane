CREATE TABLE IF NOT EXISTS tenants (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    owner TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS config_definitions (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    config_key TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    schema JSONB NOT NULL,
    default_value JSONB,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    UNIQUE (tenant_id, config_key)
);

CREATE TABLE IF NOT EXISTS config_versions (
    id TEXT PRIMARY KEY,
    config_definition_id TEXT NOT NULL REFERENCES config_definitions(id) ON DELETE RESTRICT,
    version_number INTEGER NOT NULL CHECK (version_number > 0),
    value JSONB NOT NULL,
    source_commit_sha TEXT NOT NULL DEFAULT '',
    created_by TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL,
    UNIQUE (config_definition_id, version_number)
);

CREATE TABLE IF NOT EXISTS config_environment_states (
    config_definition_id TEXT NOT NULL REFERENCES config_definitions(id) ON DELETE RESTRICT,
    environment TEXT NOT NULL CHECK (environment IN ('development', 'staging', 'production')),
    stable_version_id TEXT REFERENCES config_versions(id) ON DELETE RESTRICT,
    active_rollout_id TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (config_definition_id, environment)
);

INSERT INTO schema_migrations (version, name)
VALUES (2, 'config_registry')
ON CONFLICT (version) DO NOTHING;

CREATE TABLE IF NOT EXISTS rollouts (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    config_definition_id TEXT NOT NULL REFERENCES config_definitions(id) ON DELETE RESTRICT,
    config_key TEXT NOT NULL,
    environment TEXT NOT NULL CHECK (environment IN ('development', 'staging', 'production')),
    target_services JSONB NOT NULL DEFAULT '[]',
    stable_version_id TEXT NOT NULL REFERENCES config_versions(id) ON DELETE RESTRICT,
    candidate_version_id TEXT NOT NULL REFERENCES config_versions(id) ON DELETE RESTRICT,
    candidate_version_number INTEGER NOT NULL CHECK (candidate_version_number > 0),
    state TEXT NOT NULL CHECK (state IN (
        'PENDING',
        'VALIDATING',
        'READY',
        'DEPLOYING',
        'EVALUATING',
        'PROMOTING',
        'PAUSED',
        'COMPLETED',
        'ROLLING_BACK',
        'ROLLED_BACK',
        'FAILED'
    )),
    current_stage_id TEXT NOT NULL DEFAULT '',
    current_stage_index INTEGER NOT NULL DEFAULT 0 CHECK (current_stage_index >= 0),
    required_ack_percentage DOUBLE PRECISION NOT NULL DEFAULT 95,
    stage_started_at TIMESTAMPTZ NOT NULL,
    deployment_timeout_seconds INTEGER NOT NULL DEFAULT 90 CHECK (deployment_timeout_seconds >= 0),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS rollout_stages (
    id TEXT PRIMARY KEY,
    rollout_id TEXT NOT NULL REFERENCES rollouts(id) ON DELETE CASCADE,
    stage_order INTEGER NOT NULL CHECK (stage_order > 0),
    percentage INTEGER NOT NULL CHECK (percentage > 0 AND percentage <= 100),
    minimum_duration_seconds INTEGER NOT NULL CHECK (minimum_duration_seconds >= 0),
    activated_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    UNIQUE (rollout_id, stage_order),
    UNIQUE (rollout_id, percentage)
);

CREATE TABLE IF NOT EXISTS rollout_stage_targets (
    rollout_id TEXT NOT NULL REFERENCES rollouts(id) ON DELETE CASCADE,
    stage_id TEXT NOT NULL REFERENCES rollout_stages(id) ON DELETE CASCADE,
    agent_id TEXT NOT NULL REFERENCES agents(id) ON DELETE RESTRICT,
    bucket INTEGER NOT NULL CHECK (bucket >= 0 AND bucket < 10000),
    expected_version_id TEXT NOT NULL REFERENCES config_versions(id) ON DELETE RESTRICT,
    snapshot_revision BIGINT NOT NULL CHECK (snapshot_revision > 0),
    status TEXT NOT NULL CHECK (status IN ('PENDING', 'ACKED')),
    created_at TIMESTAMPTZ NOT NULL,
    acked_at TIMESTAMPTZ,
    PRIMARY KEY (rollout_id, stage_id, agent_id),
    UNIQUE (rollout_id, stage_id, snapshot_revision, expected_version_id, agent_id)
);

CREATE UNIQUE INDEX IF NOT EXISTS one_active_rollout_per_config_environment
ON rollouts (config_definition_id, environment)
WHERE state IN (
    'PENDING',
    'VALIDATING',
    'READY',
    'DEPLOYING',
    'EVALUATING',
    'PROMOTING',
    'PAUSED',
    'ROLLING_BACK'
);

CREATE SEQUENCE IF NOT EXISTS snapshot_revisions START WITH 1;

INSERT INTO schema_migrations (version, name)
VALUES (4, 'rollouts')
ON CONFLICT (version) DO NOTHING;

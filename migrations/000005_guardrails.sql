ALTER TABLE rollouts
ADD COLUMN IF NOT EXISTS guardrails JSONB NOT NULL DEFAULT '[]',
ADD COLUMN IF NOT EXISTS guardrail_failures JSONB NOT NULL DEFAULT '{}',
ADD COLUMN IF NOT EXISTS rollout_max_duration_seconds INTEGER NOT NULL DEFAULT 900 CHECK (rollout_max_duration_seconds >= 0),
ADD COLUMN IF NOT EXISTS rollback_timeout_seconds INTEGER NOT NULL DEFAULT 120 CHECK (rollback_timeout_seconds >= 0),
ADD COLUMN IF NOT EXISTS rollback_status TEXT NOT NULL DEFAULT '' CHECK (rollback_status IN ('', 'VERIFIED', 'PARTIAL'));

ALTER TABLE rollout_stages
DROP CONSTRAINT IF EXISTS rollout_stages_rollout_id_percentage_key;

INSERT INTO schema_migrations (version, name)
VALUES (5, 'guardrails')
ON CONFLICT (version) DO NOTHING;

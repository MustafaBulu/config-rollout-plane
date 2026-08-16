package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"config-rollout-plane/internal/agentregistry"
	"config-rollout-plane/internal/configregistry"
	"config-rollout-plane/internal/domain"
	rolloutpkg "config-rollout-plane/internal/rollout"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(ctx context.Context, databaseURL string) (*Store, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return &Store{pool: pool}, nil
}

func (s *Store) Close() {
	s.pool.Close()
}

func (s *Store) Check(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

func (s *Store) SaveAgent(ctx context.Context, agent domain.Agent) error {
	labels, err := json.Marshal(agent.Labels)
	if err != nil {
		return fmt.Errorf("%w: labels", agentregistry.ErrInvalidInput)
	}

	_, err = s.pool.Exec(ctx, `
		INSERT INTO agents (id, service, environment, zone, instance, labels, registered_at, last_seen_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (id)
		DO UPDATE SET
			service = EXCLUDED.service,
			environment = EXCLUDED.environment,
			zone = EXCLUDED.zone,
			instance = EXCLUDED.instance,
			labels = EXCLUDED.labels,
			last_seen_at = EXCLUDED.last_seen_at
	`, agent.ID, agent.Service, string(agent.Environment), agent.Zone, agent.Instance, labels, agent.RegisteredAt, agent.LastSeenAt)
	return mapAgentError(err)
}

func (s *Store) GetAgent(ctx context.Context, agentID string) (domain.Agent, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, service, environment, zone, instance, labels, registered_at, last_seen_at
		FROM agents
		WHERE id = $1
	`, agentID)

	agent, err := scanAgent(row)
	return agent, mapAgentError(err)
}

func (s *Store) ListAgents(ctx context.Context) ([]domain.Agent, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, service, environment, zone, instance, labels, registered_at, last_seen_at
		FROM agents
		ORDER BY id
	`)
	if err != nil {
		return nil, mapAgentError(err)
	}
	defer rows.Close()

	var agents []domain.Agent
	for rows.Next() {
		agent, err := scanAgent(rows)
		if err != nil {
			return nil, err
		}
		agents = append(agents, agent)
	}
	return agents, mapAgentError(rows.Err())
}

func (s *Store) SaveCredential(ctx context.Context, credential domain.AgentCredential) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO agent_credentials (token, agent_id, expires_at, created_at)
		VALUES ($1, $2, $3, $4)
	`, credential.Token, credential.AgentID, credential.ExpiresAt, credential.CreatedAt)
	return mapAgentError(err)
}

func (s *Store) GetCredential(ctx context.Context, token string) (domain.AgentCredential, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT token, agent_id, expires_at, created_at
		FROM agent_credentials
		WHERE token = $1
	`, token)

	credential, err := scanCredential(row)
	return credential, mapAgentError(err)
}

func (s *Store) TouchHeartbeat(ctx context.Context, agentID string, seenAt time.Time) (domain.Agent, error) {
	row := s.pool.QueryRow(ctx, `
		UPDATE agents
		SET last_seen_at = $2
		WHERE id = $1
		RETURNING id, service, environment, zone, instance, labels, registered_at, last_seen_at
	`, agentID, seenAt)

	agent, err := scanAgent(row)
	return agent, mapAgentError(err)
}

func (s *Store) SaveAcknowledgement(ctx context.Context, acknowledgement domain.AgentAcknowledgement) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO agent_acknowledgements (
			id, agent_id, config_definition_id, version_id, snapshot_revision, counted, created_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (agent_id, config_definition_id, version_id, snapshot_revision)
		DO NOTHING
	`, acknowledgement.ID, acknowledgement.AgentID, acknowledgement.ConfigDefinitionID, acknowledgement.VersionID, acknowledgement.SnapshotRevision, acknowledgement.Counted, acknowledgement.CreatedAt)
	return mapAgentError(err)
}

func (s *Store) CreateTenant(ctx context.Context, tenant domain.Tenant) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO tenants (id, name, description, owner, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, tenant.ID, tenant.Name, tenant.Description, tenant.Owner, tenant.CreatedAt, tenant.UpdatedAt)
	return mapError(err)
}

func (s *Store) ListTenants(ctx context.Context) ([]domain.Tenant, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, name, description, owner, created_at, updated_at
		FROM tenants
		ORDER BY id
	`)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()

	var tenants []domain.Tenant
	for rows.Next() {
		tenant, err := scanTenant(rows)
		if err != nil {
			return nil, err
		}
		tenants = append(tenants, tenant)
	}
	return tenants, mapError(rows.Err())
}

func (s *Store) GetTenant(ctx context.Context, tenantID string) (domain.Tenant, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, name, description, owner, created_at, updated_at
		FROM tenants
		WHERE id = $1
	`, tenantID)

	tenant, err := scanTenant(row)
	return tenant, mapError(err)
}

func (s *Store) CreateDefinition(ctx context.Context, definition domain.ConfigDefinition) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO config_definitions (
			id, tenant_id, config_key, description, schema, default_value, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, definition.ID, definition.TenantID, definition.Key, definition.Description, definition.Schema, nullableJSON(definition.DefaultValue), definition.CreatedAt, definition.UpdatedAt)
	return mapError(err)
}

func (s *Store) ListDefinitions(ctx context.Context, tenantID string) ([]domain.ConfigDefinition, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, tenant_id, config_key, description, schema, default_value, created_at, updated_at
		FROM config_definitions
		WHERE tenant_id = $1
		ORDER BY config_key
	`, tenantID)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()

	var definitions []domain.ConfigDefinition
	for rows.Next() {
		definition, err := scanDefinition(rows)
		if err != nil {
			return nil, err
		}
		definitions = append(definitions, definition)
	}
	return definitions, mapError(rows.Err())
}

func (s *Store) GetDefinitionByKey(ctx context.Context, tenantID string, key string) (domain.ConfigDefinition, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, tenant_id, config_key, description, schema, default_value, created_at, updated_at
		FROM config_definitions
		WHERE tenant_id = $1 AND config_key = $2
	`, tenantID, key)

	definition, err := scanDefinition(row)
	return definition, mapError(err)
}

func (s *Store) CreateVersion(ctx context.Context, version domain.ConfigVersion) (domain.ConfigVersion, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.ConfigVersion{}, mapError(err)
	}
	defer tx.Rollback(ctx)

	var definitionID string
	if err := tx.QueryRow(ctx, `
		SELECT id
		FROM config_definitions
		WHERE id = $1
		FOR UPDATE
	`, version.ConfigDefinitionID).Scan(&definitionID); err != nil {
		return domain.ConfigVersion{}, mapError(err)
	}

	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(MAX(version_number), 0) + 1
		FROM config_versions
		WHERE config_definition_id = $1
	`, version.ConfigDefinitionID).Scan(&version.VersionNumber); err != nil {
		return domain.ConfigVersion{}, mapError(err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO config_versions (
			id, config_definition_id, version_number, value, source_commit_sha, created_by, created_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, version.ID, version.ConfigDefinitionID, version.VersionNumber, version.Value, version.SourceCommitSHA, version.CreatedBy, version.CreatedAt); err != nil {
		return domain.ConfigVersion{}, mapError(err)
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.ConfigVersion{}, mapError(err)
	}
	return version, nil
}

func (s *Store) ListVersions(ctx context.Context, configDefinitionID string) ([]domain.ConfigVersion, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, config_definition_id, version_number, value, source_commit_sha, created_by, created_at
		FROM config_versions
		WHERE config_definition_id = $1
		ORDER BY version_number
	`, configDefinitionID)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()

	var versions []domain.ConfigVersion
	for rows.Next() {
		version, err := scanVersion(rows)
		if err != nil {
			return nil, err
		}
		versions = append(versions, version)
	}
	return versions, mapError(rows.Err())
}

func (s *Store) GetVersionByNumber(ctx context.Context, configDefinitionID string, versionNumber int) (domain.ConfigVersion, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, config_definition_id, version_number, value, source_commit_sha, created_by, created_at
		FROM config_versions
		WHERE config_definition_id = $1 AND version_number = $2
	`, configDefinitionID, versionNumber)

	version, err := scanVersion(row)
	return version, mapError(err)
}

func (s *Store) SaveEnvironmentState(ctx context.Context, state domain.ConfigEnvironmentState) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO config_environment_states (
			config_definition_id, environment, stable_version_id, active_rollout_id, updated_at
		)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (config_definition_id, environment)
		DO UPDATE SET
			stable_version_id = EXCLUDED.stable_version_id,
			active_rollout_id = EXCLUDED.active_rollout_id,
			updated_at = EXCLUDED.updated_at
	`, state.ConfigDefinitionID, string(state.Environment), nullableString(state.StableVersionID), state.ActiveRolloutID, state.UpdatedAt)
	return mapError(err)
}

func (s *Store) GetEnvironmentState(ctx context.Context, configDefinitionID string, environment domain.Environment) (domain.ConfigEnvironmentState, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT config_definition_id, environment, stable_version_id, active_rollout_id, updated_at
		FROM config_environment_states
		WHERE config_definition_id = $1 AND environment = $2
	`, configDefinitionID, string(environment))

	state, err := scanEnvironmentState(row)
	return state, mapError(err)
}

func (s *Store) CreateRollout(ctx context.Context, rollout rolloutpkg.Rollout, stages []rolloutpkg.Stage) error {
	targetServices, err := json.Marshal(rollout.TargetServices)
	if err != nil {
		return fmt.Errorf("%w: target services", rolloutpkg.ErrInvalidInput)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return mapRolloutError(err)
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, `
		INSERT INTO rollouts (
			id,
			tenant_id,
			config_definition_id,
			config_key,
			environment,
			target_services,
			stable_version_id,
			candidate_version_id,
			candidate_version_number,
			state,
			current_stage_id,
			current_stage_index,
			required_ack_percentage,
			stage_started_at,
			deployment_timeout_seconds,
			created_at,
			updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
	`, rollout.ID, rollout.TenantID, rollout.ConfigDefinitionID, rollout.ConfigKey, string(rollout.Environment), targetServices, rollout.StableVersionID, rollout.CandidateVersionID, rollout.CandidateVersion, string(rollout.State), rollout.CurrentStageID, rollout.CurrentStageIndex, rollout.RequiredAckPercent, rollout.StageStartedAt, durationSeconds(rollout.DeploymentTimeout), rollout.CreatedAt, rollout.UpdatedAt)
	if err != nil {
		return mapRolloutError(err)
	}

	for i, stage := range stages {
		_, err := tx.Exec(ctx, `
			INSERT INTO rollout_stages (
				id, rollout_id, stage_order, percentage, minimum_duration_seconds
			)
			VALUES ($1, $2, $3, $4, $5)
		`, stage.ID, rollout.ID, i+1, stage.Percentage, durationSeconds(stage.MinimumDuration))
		if err != nil {
			return mapRolloutError(err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return mapRolloutError(err)
	}
	return nil
}

func (s *Store) GetRollout(ctx context.Context, rolloutID string) (rolloutpkg.Rollout, error) {
	row := s.pool.QueryRow(ctx, rolloutSelectSQL()+`
		WHERE id = $1
	`, rolloutID)

	rollout, err := scanRollout(row)
	return rollout, mapRolloutError(err)
}

func (s *Store) GetActiveRollout(ctx context.Context, configDefinitionID string, environment string) (rolloutpkg.Rollout, error) {
	row := s.pool.QueryRow(ctx, rolloutSelectSQL()+`
		WHERE config_definition_id = $1
		  AND environment = $2
		  AND state IN ('PENDING', 'VALIDATING', 'READY', 'DEPLOYING', 'EVALUATING', 'PROMOTING', 'PAUSED', 'ROLLING_BACK')
		ORDER BY created_at DESC
		LIMIT 1
	`, configDefinitionID, environment)

	rollout, err := scanRollout(row)
	return rollout, mapRolloutError(err)
}

func (s *Store) ListActiveRollouts(ctx context.Context) ([]rolloutpkg.Rollout, error) {
	rows, err := s.pool.Query(ctx, rolloutSelectSQL()+`
		WHERE state IN ('PENDING', 'VALIDATING', 'READY', 'DEPLOYING', 'EVALUATING', 'PROMOTING', 'PAUSED', 'ROLLING_BACK')
		ORDER BY created_at
	`)
	if err != nil {
		return nil, mapRolloutError(err)
	}
	defer rows.Close()

	var rollouts []rolloutpkg.Rollout
	for rows.Next() {
		rollout, err := scanRollout(rows)
		if err != nil {
			return nil, err
		}
		rollouts = append(rollouts, rollout)
	}
	return rollouts, mapRolloutError(rows.Err())
}

func (s *Store) UpdateRollout(ctx context.Context, rollout rolloutpkg.Rollout) error {
	targetServices, err := json.Marshal(rollout.TargetServices)
	if err != nil {
		return fmt.Errorf("%w: target services", rolloutpkg.ErrInvalidInput)
	}

	tag, err := s.pool.Exec(ctx, `
		UPDATE rollouts
		SET tenant_id = $2,
			config_definition_id = $3,
			config_key = $4,
			environment = $5,
			target_services = $6,
			stable_version_id = $7,
			candidate_version_id = $8,
			candidate_version_number = $9,
			state = $10,
			current_stage_id = $11,
			current_stage_index = $12,
			required_ack_percentage = $13,
			stage_started_at = $14,
			deployment_timeout_seconds = $15,
			updated_at = $16
		WHERE id = $1
	`, rollout.ID, rollout.TenantID, rollout.ConfigDefinitionID, rollout.ConfigKey, string(rollout.Environment), targetServices, rollout.StableVersionID, rollout.CandidateVersionID, rollout.CandidateVersion, string(rollout.State), rollout.CurrentStageID, rollout.CurrentStageIndex, rollout.RequiredAckPercent, rollout.StageStartedAt, durationSeconds(rollout.DeploymentTimeout), rollout.UpdatedAt)
	if err != nil {
		return mapRolloutError(err)
	}
	if tag.RowsAffected() == 0 {
		return rolloutpkg.ErrNotFound
	}
	return nil
}

func (s *Store) ListStages(ctx context.Context, rolloutID string) ([]rolloutpkg.Stage, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, percentage, minimum_duration_seconds
		FROM rollout_stages
		WHERE rollout_id = $1
		ORDER BY stage_order
	`, rolloutID)
	if err != nil {
		return nil, mapRolloutError(err)
	}
	defer rows.Close()

	var stages []rolloutpkg.Stage
	for rows.Next() {
		stage, err := scanStage(rows)
		if err != nil {
			return nil, err
		}
		stages = append(stages, stage)
	}
	return stages, mapRolloutError(rows.Err())
}

func (s *Store) SaveStageTargets(ctx context.Context, targets []rolloutpkg.StageTarget) error {
	for _, target := range targets {
		if err := s.insertStageTarget(ctx, target); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) ReplaceStageTargets(ctx context.Context, rolloutID string, stageID string, targets []rolloutpkg.StageTarget) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return mapRolloutError(err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `
		DELETE FROM rollout_stage_targets
		WHERE rollout_id = $1 AND stage_id = $2
	`, rolloutID, stageID); err != nil {
		return mapRolloutError(err)
	}

	for _, target := range targets {
		if err := insertStageTarget(ctx, tx, target); err != nil {
			return err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return mapRolloutError(err)
	}
	return nil
}

func (s *Store) ListStageTargets(ctx context.Context, rolloutID string, stageID string) ([]rolloutpkg.StageTarget, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT rollout_id, stage_id, agent_id, bucket, expected_version_id, snapshot_revision, created_at, acked_at, status
		FROM rollout_stage_targets
		WHERE rollout_id = $1 AND stage_id = $2
		ORDER BY bucket, agent_id
	`, rolloutID, stageID)
	if err != nil {
		return nil, mapRolloutError(err)
	}
	defer rows.Close()

	var targets []rolloutpkg.StageTarget
	for rows.Next() {
		target, err := scanStageTarget(rows)
		if err != nil {
			return nil, err
		}
		targets = append(targets, target)
	}
	return targets, mapRolloutError(rows.Err())
}

func (s *Store) NextSnapshotRevision(ctx context.Context) (int64, error) {
	var revision int64
	if err := s.pool.QueryRow(ctx, `SELECT nextval('snapshot_revisions')`).Scan(&revision); err != nil {
		return 0, mapRolloutError(err)
	}
	return revision, nil
}

func (s *Store) insertStageTarget(ctx context.Context, target rolloutpkg.StageTarget) error {
	return insertStageTarget(ctx, s.pool, target)
}

type rowScanner interface {
	Scan(dest ...any) error
}

type execer interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
}

func rolloutSelectSQL() string {
	return `
		SELECT
			id,
			tenant_id,
			config_definition_id,
			config_key,
			environment,
			target_services,
			stable_version_id,
			candidate_version_id,
			candidate_version_number,
			state,
			current_stage_id,
			current_stage_index,
			required_ack_percentage,
			stage_started_at,
			deployment_timeout_seconds,
			created_at,
			updated_at
		FROM rollouts
	`
}

func insertStageTarget(ctx context.Context, exec execer, target rolloutpkg.StageTarget) error {
	_, err := exec.Exec(ctx, `
		INSERT INTO rollout_stage_targets (
			rollout_id,
			stage_id,
			agent_id,
			bucket,
			expected_version_id,
			snapshot_revision,
			status,
			created_at,
			acked_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (rollout_id, stage_id, agent_id)
		DO UPDATE SET
			bucket = EXCLUDED.bucket,
			expected_version_id = EXCLUDED.expected_version_id,
			snapshot_revision = EXCLUDED.snapshot_revision,
			status = EXCLUDED.status,
			created_at = EXCLUDED.created_at,
			acked_at = EXCLUDED.acked_at
	`, target.RolloutID, target.StageID, target.AgentID, target.Bucket, target.ExpectedVersionID, target.SnapshotRevision, string(target.Status), target.CreatedAt, nullableTime(target.AckedAt))
	return mapRolloutError(err)
}

func scanAgent(row rowScanner) (domain.Agent, error) {
	var agent domain.Agent
	var environment string
	var labels []byte
	err := row.Scan(
		&agent.ID,
		&agent.Service,
		&environment,
		&agent.Zone,
		&agent.Instance,
		&labels,
		&agent.RegisteredAt,
		&agent.LastSeenAt,
	)
	if err != nil {
		return domain.Agent{}, mapAgentError(err)
	}
	if len(labels) > 0 {
		if err := json.Unmarshal(labels, &agent.Labels); err != nil {
			return domain.Agent{}, err
		}
	}
	agent.Environment = domain.Environment(environment)
	return agent, nil
}

func scanCredential(row rowScanner) (domain.AgentCredential, error) {
	var credential domain.AgentCredential
	err := row.Scan(&credential.Token, &credential.AgentID, &credential.ExpiresAt, &credential.CreatedAt)
	return credential, mapAgentError(err)
}

func scanTenant(row rowScanner) (domain.Tenant, error) {
	var tenant domain.Tenant
	err := row.Scan(&tenant.ID, &tenant.Name, &tenant.Description, &tenant.Owner, &tenant.CreatedAt, &tenant.UpdatedAt)
	return tenant, mapError(err)
}

func scanDefinition(row rowScanner) (domain.ConfigDefinition, error) {
	var definition domain.ConfigDefinition
	err := row.Scan(
		&definition.ID,
		&definition.TenantID,
		&definition.Key,
		&definition.Description,
		&definition.Schema,
		&definition.DefaultValue,
		&definition.CreatedAt,
		&definition.UpdatedAt,
	)
	return definition, mapError(err)
}

func scanVersion(row rowScanner) (domain.ConfigVersion, error) {
	var version domain.ConfigVersion
	err := row.Scan(
		&version.ID,
		&version.ConfigDefinitionID,
		&version.VersionNumber,
		&version.Value,
		&version.SourceCommitSHA,
		&version.CreatedBy,
		&version.CreatedAt,
	)
	return version, mapError(err)
}

func scanEnvironmentState(row rowScanner) (domain.ConfigEnvironmentState, error) {
	var state domain.ConfigEnvironmentState
	var environment string
	var stableVersionID *string
	err := row.Scan(
		&state.ConfigDefinitionID,
		&environment,
		&stableVersionID,
		&state.ActiveRolloutID,
		&state.UpdatedAt,
	)
	if stableVersionID != nil {
		state.StableVersionID = *stableVersionID
	}
	state.Environment = domain.Environment(environment)
	return state, mapError(err)
}

func scanRollout(row rowScanner) (rolloutpkg.Rollout, error) {
	var rollout rolloutpkg.Rollout
	var environment string
	var state string
	var targetServices []byte
	var deploymentTimeoutSeconds int
	err := row.Scan(
		&rollout.ID,
		&rollout.TenantID,
		&rollout.ConfigDefinitionID,
		&rollout.ConfigKey,
		&environment,
		&targetServices,
		&rollout.StableVersionID,
		&rollout.CandidateVersionID,
		&rollout.CandidateVersion,
		&state,
		&rollout.CurrentStageID,
		&rollout.CurrentStageIndex,
		&rollout.RequiredAckPercent,
		&rollout.StageStartedAt,
		&deploymentTimeoutSeconds,
		&rollout.CreatedAt,
		&rollout.UpdatedAt,
	)
	if err != nil {
		return rolloutpkg.Rollout{}, mapRolloutError(err)
	}
	if len(targetServices) > 0 {
		if err := json.Unmarshal(targetServices, &rollout.TargetServices); err != nil {
			return rolloutpkg.Rollout{}, err
		}
	}
	rollout.Environment = domain.Environment(environment)
	rollout.State = rolloutpkg.State(state)
	rollout.DeploymentTimeout = time.Duration(deploymentTimeoutSeconds) * time.Second
	return rollout, nil
}

func scanStage(row rowScanner) (rolloutpkg.Stage, error) {
	var stage rolloutpkg.Stage
	var minimumDurationSeconds int
	err := row.Scan(&stage.ID, &stage.Percentage, &minimumDurationSeconds)
	if err != nil {
		return rolloutpkg.Stage{}, mapRolloutError(err)
	}
	stage.MinimumDuration = time.Duration(minimumDurationSeconds) * time.Second
	return stage, nil
}

func scanStageTarget(row rowScanner) (rolloutpkg.StageTarget, error) {
	var target rolloutpkg.StageTarget
	var status string
	var ackedAt *time.Time
	err := row.Scan(
		&target.RolloutID,
		&target.StageID,
		&target.AgentID,
		&target.Bucket,
		&target.ExpectedVersionID,
		&target.SnapshotRevision,
		&target.CreatedAt,
		&ackedAt,
		&status,
	)
	if err != nil {
		return rolloutpkg.StageTarget{}, mapRolloutError(err)
	}
	if ackedAt != nil {
		target.AckedAt = *ackedAt
	}
	target.Status = rolloutpkg.TargetStatus(status)
	return target, nil
}

func nullableJSON(value json.RawMessage) any {
	if len(value) == 0 {
		return nil
	}
	return value
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}

func durationSeconds(value time.Duration) int {
	if value <= 0 {
		return 0
	}
	return int(value / time.Second)
}

func mapError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return configregistry.ErrNotFound
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			return fmt.Errorf("%w: %s", configregistry.ErrAlreadyExists, pgErr.ConstraintName)
		case "23503":
			return fmt.Errorf("%w: %s", configregistry.ErrNotFound, pgErr.ConstraintName)
		case "23514", "22P02":
			return fmt.Errorf("%w: %s", configregistry.ErrInvalidInput, pgErr.ConstraintName)
		}
	}

	return err
}

func mapAgentError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return agentregistry.ErrNotFound
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23503":
			return fmt.Errorf("%w: %s", agentregistry.ErrNotFound, pgErr.ConstraintName)
		case "23514", "22P02":
			return fmt.Errorf("%w: %s", agentregistry.ErrInvalidInput, pgErr.ConstraintName)
		}
	}

	return err
}

func mapRolloutError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return rolloutpkg.ErrNotFound
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			return fmt.Errorf("%w: %s", rolloutpkg.ErrConflict, pgErr.ConstraintName)
		case "23503":
			return fmt.Errorf("%w: %s", rolloutpkg.ErrNotFound, pgErr.ConstraintName)
		case "23514", "22P02":
			return fmt.Errorf("%w: %s", rolloutpkg.ErrInvalidInput, pgErr.ConstraintName)
		}
	}

	return err
}

var _ configregistry.Store = (*Store)(nil)
var _ agentregistry.Store = (*Store)(nil)
var _ rolloutpkg.Store = (*Store)(nil)
var _ interface {
	Check(context.Context) error
	Close()
} = (*Store)(nil)

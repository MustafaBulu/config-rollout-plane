package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"config-rollout-plane/internal/configregistry"
	"config-rollout-plane/internal/domain"

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

type rowScanner interface {
	Scan(dest ...any) error
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

var _ configregistry.Store = (*Store)(nil)
var _ interface {
	Check(context.Context) error
	Close()
} = (*Store)(nil)

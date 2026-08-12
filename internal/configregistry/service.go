package configregistry

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"config-rollout-plane/internal/domain"
)

type Store interface {
	CreateTenant(ctx context.Context, tenant domain.Tenant) error
	ListTenants(ctx context.Context) ([]domain.Tenant, error)
	GetTenant(ctx context.Context, tenantID string) (domain.Tenant, error)

	CreateDefinition(ctx context.Context, definition domain.ConfigDefinition) error
	ListDefinitions(ctx context.Context, tenantID string) ([]domain.ConfigDefinition, error)
	GetDefinitionByKey(ctx context.Context, tenantID string, key string) (domain.ConfigDefinition, error)

	CreateVersion(ctx context.Context, version domain.ConfigVersion) (domain.ConfigVersion, error)
	ListVersions(ctx context.Context, configDefinitionID string) ([]domain.ConfigVersion, error)
	GetVersionByNumber(ctx context.Context, configDefinitionID string, versionNumber int) (domain.ConfigVersion, error)

	SaveEnvironmentState(ctx context.Context, state domain.ConfigEnvironmentState) error
	GetEnvironmentState(ctx context.Context, configDefinitionID string, environment domain.Environment) (domain.ConfigEnvironmentState, error)
}

type Service struct {
	store     Store
	validator SchemaValidator
	now       func() time.Time
	newID     func(prefix string) string
}

func NewService(store Store, validator SchemaValidator) *Service {
	return &Service{
		store:     store,
		validator: validator,
		now:       func() time.Time { return time.Now().UTC() },
		newID:     randomID,
	}
}

type CreateTenantInput struct {
	ID          string
	Name        string
	Description string
	Owner       string
}

func (s *Service) CreateTenant(ctx context.Context, input CreateTenantInput) (domain.Tenant, error) {
	now := s.now()
	tenant := domain.Tenant{
		ID:          input.ID,
		Name:        input.Name,
		Description: input.Description,
		Owner:       input.Owner,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := tenant.Validate(); err != nil {
		return domain.Tenant{}, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	if err := s.store.CreateTenant(ctx, tenant); err != nil {
		return domain.Tenant{}, err
	}
	return tenant, nil
}

func (s *Service) ListTenants(ctx context.Context) ([]domain.Tenant, error) {
	return s.store.ListTenants(ctx)
}

func (s *Service) GetTenant(ctx context.Context, tenantID string) (domain.Tenant, error) {
	return s.store.GetTenant(ctx, tenantID)
}

type CreateDefinitionInput struct {
	TenantID     string
	Key          string
	Description  string
	Schema       json.RawMessage
	DefaultValue json.RawMessage
}

func (s *Service) CreateDefinition(ctx context.Context, input CreateDefinitionInput) (domain.ConfigDefinition, error) {
	if _, err := s.store.GetTenant(ctx, input.TenantID); err != nil {
		return domain.ConfigDefinition{}, err
	}

	if err := s.validator.ValidateSchema(input.Schema); err != nil {
		return domain.ConfigDefinition{}, err
	}
	if len(input.DefaultValue) > 0 {
		if err := s.validator.ValidateValue(input.Schema, input.DefaultValue); err != nil {
			return domain.ConfigDefinition{}, err
		}
	}

	now := s.now()
	definition := domain.ConfigDefinition{
		ID:           s.newID("cfg"),
		TenantID:     input.TenantID,
		Key:          input.Key,
		Description:  input.Description,
		Schema:       cloneRaw(input.Schema),
		DefaultValue: cloneRaw(input.DefaultValue),
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if err := definition.Validate(); err != nil {
		return domain.ConfigDefinition{}, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	if err := s.store.CreateDefinition(ctx, definition); err != nil {
		return domain.ConfigDefinition{}, err
	}
	return definition, nil
}

func (s *Service) ListDefinitions(ctx context.Context, tenantID string) ([]domain.ConfigDefinition, error) {
	if _, err := s.store.GetTenant(ctx, tenantID); err != nil {
		return nil, err
	}
	return s.store.ListDefinitions(ctx, tenantID)
}

func (s *Service) GetDefinition(ctx context.Context, tenantID string, key string) (domain.ConfigDefinition, error) {
	return s.store.GetDefinitionByKey(ctx, tenantID, key)
}

type CreateVersionInput struct {
	TenantID        string
	Key             string
	Value           json.RawMessage
	SourceCommitSHA string
	CreatedBy       string
}

func (s *Service) CreateVersion(ctx context.Context, input CreateVersionInput) (domain.ConfigVersion, error) {
	definition, err := s.store.GetDefinitionByKey(ctx, input.TenantID, input.Key)
	if err != nil {
		return domain.ConfigVersion{}, err
	}

	if err := s.validator.ValidateValue(definition.Schema, input.Value); err != nil {
		return domain.ConfigVersion{}, err
	}

	version := domain.ConfigVersion{
		ID:                 s.newID("ver"),
		ConfigDefinitionID: definition.ID,
		Value:              cloneRaw(input.Value),
		SourceCommitSHA:    input.SourceCommitSHA,
		CreatedBy:          input.CreatedBy,
		CreatedAt:          s.now(),
	}

	created, err := s.store.CreateVersion(ctx, version)
	if err != nil {
		return domain.ConfigVersion{}, err
	}
	return created, nil
}

func (s *Service) ListVersions(ctx context.Context, tenantID string, key string) ([]domain.ConfigVersion, error) {
	definition, err := s.store.GetDefinitionByKey(ctx, tenantID, key)
	if err != nil {
		return nil, err
	}
	return s.store.ListVersions(ctx, definition.ID)
}

type SetStableVersionInput struct {
	TenantID      string
	Key           string
	Environment   domain.Environment
	VersionNumber int
}

func (s *Service) SetStableVersion(ctx context.Context, input SetStableVersionInput) (domain.ConfigEnvironmentState, error) {
	if err := input.Environment.Validate(); err != nil {
		return domain.ConfigEnvironmentState{}, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}

	definition, err := s.store.GetDefinitionByKey(ctx, input.TenantID, input.Key)
	if err != nil {
		return domain.ConfigEnvironmentState{}, err
	}

	version, err := s.store.GetVersionByNumber(ctx, definition.ID, input.VersionNumber)
	if err != nil {
		return domain.ConfigEnvironmentState{}, err
	}

	state, err := s.store.GetEnvironmentState(ctx, definition.ID, input.Environment)
	if err != nil {
		if !errors.Is(err, ErrNotFound) {
			return domain.ConfigEnvironmentState{}, err
		}
		state = domain.ConfigEnvironmentState{
			ConfigDefinitionID: definition.ID,
			Environment:        input.Environment,
		}
	}

	next, err := state.WithStableVersion(version.ID)
	if err != nil {
		return domain.ConfigEnvironmentState{}, fmt.Errorf("%w: %v", ErrConflict, err)
	}
	next.UpdatedAt = s.now()

	if err := next.Validate(); err != nil {
		return domain.ConfigEnvironmentState{}, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	if err := s.store.SaveEnvironmentState(ctx, next); err != nil {
		return domain.ConfigEnvironmentState{}, err
	}
	return next, nil
}

func (s *Service) GetEnvironmentState(ctx context.Context, tenantID string, key string, environment domain.Environment) (domain.ConfigEnvironmentState, error) {
	definition, err := s.store.GetDefinitionByKey(ctx, tenantID, key)
	if err != nil {
		return domain.ConfigEnvironmentState{}, err
	}
	return s.store.GetEnvironmentState(ctx, definition.ID, environment)
}

func cloneRaw(value json.RawMessage) json.RawMessage {
	if len(value) == 0 {
		return nil
	}
	cloned := make([]byte, len(value))
	copy(cloned, value)
	return cloned
}

func randomID(prefix string) string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return prefix + "_" + hex.EncodeToString(b[:])
}

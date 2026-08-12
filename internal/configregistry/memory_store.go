package configregistry

import (
	"context"
	"fmt"
	"slices"
	"sync"

	"config-rollout-plane/internal/domain"
)

type MemoryStore struct {
	mu sync.RWMutex

	tenants map[string]domain.Tenant

	definitions      map[string]domain.ConfigDefinition
	definitionsByKey map[string]string

	versions         map[string][]domain.ConfigVersion
	versionsByNumber map[string]domain.ConfigVersion

	states map[string]domain.ConfigEnvironmentState
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		tenants:          make(map[string]domain.Tenant),
		definitions:      make(map[string]domain.ConfigDefinition),
		definitionsByKey: make(map[string]string),
		versions:         make(map[string][]domain.ConfigVersion),
		versionsByNumber: make(map[string]domain.ConfigVersion),
		states:           make(map[string]domain.ConfigEnvironmentState),
	}
}

func (s *MemoryStore) CreateTenant(ctx context.Context, tenant domain.Tenant) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.tenants[tenant.ID]; ok {
		return fmt.Errorf("%w: tenant %q", ErrAlreadyExists, tenant.ID)
	}

	s.tenants[tenant.ID] = tenant
	return nil
}

func (s *MemoryStore) ListTenants(ctx context.Context) ([]domain.Tenant, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	tenants := make([]domain.Tenant, 0, len(s.tenants))
	for _, tenant := range s.tenants {
		tenants = append(tenants, tenant)
	}
	slices.SortFunc(tenants, func(a, b domain.Tenant) int {
		return cmpString(a.ID, b.ID)
	})
	return tenants, nil
}

func (s *MemoryStore) GetTenant(ctx context.Context, tenantID string) (domain.Tenant, error) {
	if err := ctx.Err(); err != nil {
		return domain.Tenant{}, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	tenant, ok := s.tenants[tenantID]
	if !ok {
		return domain.Tenant{}, fmt.Errorf("%w: tenant %q", ErrNotFound, tenantID)
	}
	return tenant, nil
}

func (s *MemoryStore) CreateDefinition(ctx context.Context, definition domain.ConfigDefinition) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.tenants[definition.TenantID]; !ok {
		return fmt.Errorf("%w: tenant %q", ErrNotFound, definition.TenantID)
	}
	if _, ok := s.definitions[definition.ID]; ok {
		return fmt.Errorf("%w: definition %q", ErrAlreadyExists, definition.ID)
	}

	key := definitionKey(definition.TenantID, definition.Key)
	if _, ok := s.definitionsByKey[key]; ok {
		return fmt.Errorf("%w: config key %q", ErrAlreadyExists, definition.Key)
	}

	definition.Schema = cloneRaw(definition.Schema)
	definition.DefaultValue = cloneRaw(definition.DefaultValue)
	s.definitions[definition.ID] = definition
	s.definitionsByKey[key] = definition.ID
	return nil
}

func (s *MemoryStore) ListDefinitions(ctx context.Context, tenantID string) ([]domain.ConfigDefinition, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	if _, ok := s.tenants[tenantID]; !ok {
		return nil, fmt.Errorf("%w: tenant %q", ErrNotFound, tenantID)
	}

	definitions := make([]domain.ConfigDefinition, 0)
	for _, definition := range s.definitions {
		if definition.TenantID == tenantID {
			definition.Schema = cloneRaw(definition.Schema)
			definition.DefaultValue = cloneRaw(definition.DefaultValue)
			definitions = append(definitions, definition)
		}
	}
	slices.SortFunc(definitions, func(a, b domain.ConfigDefinition) int {
		return cmpString(a.Key, b.Key)
	})
	return definitions, nil
}

func (s *MemoryStore) GetDefinitionByKey(ctx context.Context, tenantID string, key string) (domain.ConfigDefinition, error) {
	if err := ctx.Err(); err != nil {
		return domain.ConfigDefinition{}, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	id, ok := s.definitionsByKey[definitionKey(tenantID, key)]
	if !ok {
		return domain.ConfigDefinition{}, fmt.Errorf("%w: config %q", ErrNotFound, key)
	}

	definition := s.definitions[id]
	definition.Schema = cloneRaw(definition.Schema)
	definition.DefaultValue = cloneRaw(definition.DefaultValue)
	return definition, nil
}

func (s *MemoryStore) CreateVersion(ctx context.Context, version domain.ConfigVersion) (domain.ConfigVersion, error) {
	if err := ctx.Err(); err != nil {
		return domain.ConfigVersion{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.definitions[version.ConfigDefinitionID]; !ok {
		return domain.ConfigVersion{}, fmt.Errorf("%w: definition %q", ErrNotFound, version.ConfigDefinitionID)
	}

	version.VersionNumber = len(s.versions[version.ConfigDefinitionID]) + 1
	if err := version.Validate(); err != nil {
		return domain.ConfigVersion{}, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}

	version.Value = cloneRaw(version.Value)
	s.versions[version.ConfigDefinitionID] = append(s.versions[version.ConfigDefinitionID], version)
	s.versionsByNumber[versionNumberKey(version.ConfigDefinitionID, version.VersionNumber)] = version
	return version, nil
}

func (s *MemoryStore) ListVersions(ctx context.Context, configDefinitionID string) ([]domain.ConfigVersion, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	if _, ok := s.definitions[configDefinitionID]; !ok {
		return nil, fmt.Errorf("%w: definition %q", ErrNotFound, configDefinitionID)
	}

	versions := append([]domain.ConfigVersion(nil), s.versions[configDefinitionID]...)
	for i := range versions {
		versions[i].Value = cloneRaw(versions[i].Value)
	}
	return versions, nil
}

func (s *MemoryStore) GetVersionByNumber(ctx context.Context, configDefinitionID string, versionNumber int) (domain.ConfigVersion, error) {
	if err := ctx.Err(); err != nil {
		return domain.ConfigVersion{}, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	version, ok := s.versionsByNumber[versionNumberKey(configDefinitionID, versionNumber)]
	if !ok {
		return domain.ConfigVersion{}, fmt.Errorf("%w: version %d", ErrNotFound, versionNumber)
	}
	version.Value = cloneRaw(version.Value)
	return version, nil
}

func (s *MemoryStore) SaveEnvironmentState(ctx context.Context, state domain.ConfigEnvironmentState) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.states[stateKey(state.ConfigDefinitionID, state.Environment)] = state
	return nil
}

func (s *MemoryStore) GetEnvironmentState(ctx context.Context, configDefinitionID string, environment domain.Environment) (domain.ConfigEnvironmentState, error) {
	if err := ctx.Err(); err != nil {
		return domain.ConfigEnvironmentState{}, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	state, ok := s.states[stateKey(configDefinitionID, environment)]
	if !ok {
		return domain.ConfigEnvironmentState{}, fmt.Errorf("%w: environment state %q", ErrNotFound, environment)
	}
	return state, nil
}

func definitionKey(tenantID string, key string) string {
	return tenantID + "\x00" + key
}

func versionNumberKey(configDefinitionID string, versionNumber int) string {
	return fmt.Sprintf("%s\x00%d", configDefinitionID, versionNumber)
}

func stateKey(configDefinitionID string, environment domain.Environment) string {
	return configDefinitionID + "\x00" + string(environment)
}

func cmpString(a, b string) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

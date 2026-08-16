package dataplane

import (
	"context"
	"errors"
	"time"

	"config-rollout-plane/internal/agentregistry"
	"config-rollout-plane/internal/configregistry"
	"config-rollout-plane/internal/domain"
	"config-rollout-plane/internal/rollout"
)

type RegistrySnapshotSource interface {
	ListTenants(ctx context.Context) ([]domain.Tenant, error)
	ListDefinitions(ctx context.Context, tenantID string) ([]domain.ConfigDefinition, error)
	ListVersions(ctx context.Context, tenantID string, key string) ([]domain.ConfigVersion, error)
	GetEnvironmentState(ctx context.Context, tenantID string, key string, environment domain.Environment) (domain.ConfigEnvironmentState, error)
}

type AgentSnapshotSource interface {
	GetAgent(ctx context.Context, agentID string) (domain.Agent, error)
}

type ActiveRolloutSource interface {
	ActiveRollout(ctx context.Context, configDefinitionID string, environment domain.Environment) (rollout.Rollout, []rollout.StageTarget, error)
}

type DynamicSnapshotStore struct {
	registry RegistrySnapshotSource
	agents   AgentSnapshotSource
	rollouts ActiveRolloutSource
	tenants  []string
	now      func() time.Time
}

func NewDynamicSnapshotStore(registry RegistrySnapshotSource, agents AgentSnapshotSource, rollouts ActiveRolloutSource, tenants []string) *DynamicSnapshotStore {
	return &DynamicSnapshotStore{
		registry: registry,
		agents:   agents,
		rollouts: rollouts,
		tenants:  append([]string(nil), tenants...),
		now:      func() time.Time { return time.Now().UTC() },
	}
}

func (s *DynamicSnapshotStore) GetSnapshot(ctx context.Context, agentID string) (Snapshot, error) {
	agent, err := s.agents.GetAgent(ctx, agentID)
	if err != nil {
		if errors.Is(err, agentregistry.ErrNotFound) {
			return Snapshot{}, ErrSnapshotNotFound
		}
		return Snapshot{}, err
	}

	tenants, err := s.snapshotTenants(ctx)
	if err != nil {
		return Snapshot{}, err
	}

	revision := int64(1)
	items := make([]SnapshotItem, 0)
	for _, tenantID := range tenants {
		definitions, err := s.registry.ListDefinitions(ctx, tenantID)
		if err != nil {
			if errors.Is(err, configregistry.ErrNotFound) {
				continue
			}
			return Snapshot{}, err
		}

		for _, definition := range definitions {
			item, itemRevision, ok, err := s.snapshotItem(ctx, tenantID, definition, agent)
			if err != nil {
				return Snapshot{}, err
			}
			if !ok {
				continue
			}
			if itemRevision > revision {
				revision = itemRevision
			}
			items = append(items, item)
		}
	}

	return Snapshot{
		Revision:    revision,
		GeneratedAt: s.now(),
		Configs:     items,
	}, nil
}

func (s *DynamicSnapshotStore) snapshotItem(ctx context.Context, tenantID string, definition domain.ConfigDefinition, agent domain.Agent) (SnapshotItem, int64, bool, error) {
	state, err := s.registry.GetEnvironmentState(ctx, tenantID, definition.Key, agent.Environment)
	if err != nil {
		if errors.Is(err, configregistry.ErrNotFound) {
			return SnapshotItem{}, 0, false, nil
		}
		return SnapshotItem{}, 0, false, err
	}
	if state.StableVersionID == "" {
		return SnapshotItem{}, 0, false, nil
	}

	versions, err := s.registry.ListVersions(ctx, tenantID, definition.Key)
	if err != nil {
		return SnapshotItem{}, 0, false, err
	}

	versionID := state.StableVersionID
	assignment := Assignment{}
	revision := int64(1)

	active, targets, err := s.rollouts.ActiveRollout(ctx, definition.ID, agent.Environment)
	if err != nil {
		if !errors.Is(err, rollout.ErrNotFound) {
			return SnapshotItem{}, 0, false, err
		}
	} else {
		effective := rollout.EffectiveVersionForAgent(active, targets, agent.ID)
		versionID = effective.VersionID
		if effective.AssignedRollout != "" {
			assignment = Assignment{
				RolloutID: effective.AssignedRollout,
				StageID:   effective.AssignedStage,
			}
			revision = effective.SnapshotRevision
		}
	}

	version, ok := findVersionByID(versions, versionID)
	if !ok {
		return SnapshotItem{}, 0, false, nil
	}

	return SnapshotItem{
		ConfigDefinitionID: definition.ID,
		Key:                definition.Key,
		VersionID:          version.ID,
		Version:            version.VersionNumber,
		Value:              cloneRaw(version.Value),
		Checksum:           Checksum(version.Value),
		Assignment:         assignment,
	}, revision, true, nil
}

func (s *DynamicSnapshotStore) snapshotTenants(ctx context.Context) ([]string, error) {
	if len(s.tenants) > 0 {
		return append([]string(nil), s.tenants...), nil
	}

	tenants, err := s.registry.ListTenants(ctx)
	if err != nil {
		return nil, err
	}

	ids := make([]string, 0, len(tenants))
	for _, tenant := range tenants {
		ids = append(ids, tenant.ID)
	}
	return ids, nil
}

func findVersionByID(versions []domain.ConfigVersion, versionID string) (domain.ConfigVersion, bool) {
	for _, version := range versions {
		if version.ID == versionID {
			return version, true
		}
	}
	return domain.ConfigVersion{}, false
}

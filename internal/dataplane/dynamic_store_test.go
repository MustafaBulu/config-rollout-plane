package dataplane

import (
	"context"
	"testing"
	"time"

	"config-rollout-plane/internal/agentregistry"
	"config-rollout-plane/internal/configregistry"
	"config-rollout-plane/internal/domain"
	rolloutpkg "config-rollout-plane/internal/rollout"
)

func TestDynamicSnapshotStoreUsesFrozenRolloutTargets(t *testing.T) {
	ctx := context.Background()
	registry := configregistry.NewService(configregistry.NewMemoryStore(), configregistry.JSONSchemaValidator{})
	agents := agentregistry.NewService(agentregistry.NewMemoryStore(), "bootstrap", time.Hour)

	definition, stable, candidate := seedRegistry(t, ctx, registry)
	for i := 0; i < 200; i++ {
		registerAgent(t, ctx, agents, "agent-"+threeDigits(i), "payment-api")
	}

	rollouts := rolloutpkg.NewService(rolloutpkg.NewMemoryStore(), registry, agents)
	created, targets, err := rollouts.CreateRollout(ctx, rolloutpkg.CreateRolloutInput{
		TenantID:               "payments",
		Key:                    "payment.authorization.timeout",
		Environment:            domain.EnvironmentProduction,
		CandidateVersionNumber: candidate.VersionNumber,
		TargetServices:         []string{"payment-api"},
		Stages: []rolloutpkg.Stage{
			{ID: "stage-5", Percentage: 5},
			{ID: "stage-100", Percentage: 100},
		},
		RequiredAckPercentage: 100,
		DeploymentTimeout:     time.Minute,
	})
	if err != nil {
		t.Fatalf("create rollout: %v", err)
	}
	if len(targets) == 0 {
		t.Fatal("expected at least one frozen target")
	}

	store := NewDynamicSnapshotStore(registry, agents, rollouts, []string{"payments"})
	targetSnapshot, err := store.GetSnapshot(ctx, targets[0].AgentID)
	if err != nil {
		t.Fatalf("target snapshot: %v", err)
	}
	if len(targetSnapshot.Configs) != 1 {
		t.Fatalf("expected one config, got %d", len(targetSnapshot.Configs))
	}
	targetItem := targetSnapshot.Configs[0]
	if targetItem.ConfigDefinitionID != definition.ID {
		t.Fatalf("expected definition %q, got %q", definition.ID, targetItem.ConfigDefinitionID)
	}
	if targetItem.VersionID != candidate.ID {
		t.Fatalf("expected candidate version %q, got %q", candidate.ID, targetItem.VersionID)
	}
	if targetItem.Assignment.RolloutID != created.ID || targetItem.Assignment.StageID != created.CurrentStageID {
		t.Fatalf("expected rollout assignment, got %+v", targetItem.Assignment)
	}
	if targetSnapshot.Revision != targets[0].SnapshotRevision {
		t.Fatalf("expected snapshot revision %d, got %d", targets[0].SnapshotRevision, targetSnapshot.Revision)
	}

	registerAgent(t, ctx, agents, "new-agent", "payment-api")
	newAgentSnapshot, err := store.GetSnapshot(ctx, "new-agent")
	if err != nil {
		t.Fatalf("new agent snapshot: %v", err)
	}
	if got := newAgentSnapshot.Configs[0].VersionID; got != stable.ID {
		t.Fatalf("expected stable version for mid-stage agent %q, got %q", stable.ID, got)
	}
	if newAgentSnapshot.Configs[0].Assignment.RolloutID != "" {
		t.Fatalf("mid-stage agent should not receive rollout assignment: %+v", newAgentSnapshot.Configs[0].Assignment)
	}
}

func seedRegistry(t *testing.T, ctx context.Context, registry *configregistry.Service) (domain.ConfigDefinition, domain.ConfigVersion, domain.ConfigVersion) {
	t.Helper()

	if _, err := registry.CreateTenant(ctx, configregistry.CreateTenantInput{ID: "payments", Name: "Payments"}); err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	definition, err := registry.CreateDefinition(ctx, configregistry.CreateDefinitionInput{
		TenantID:     "payments",
		Key:          "payment.authorization.timeout",
		Schema:       []byte(`{"type":"integer","minimum":100,"maximum":10000}`),
		DefaultValue: []byte(`2000`),
	})
	if err != nil {
		t.Fatalf("create definition: %v", err)
	}
	stable, err := registry.CreateVersion(ctx, configregistry.CreateVersionInput{
		TenantID:  "payments",
		Key:       "payment.authorization.timeout",
		Value:     []byte(`2000`),
		CreatedBy: "test",
	})
	if err != nil {
		t.Fatalf("create stable version: %v", err)
	}
	candidate, err := registry.CreateVersion(ctx, configregistry.CreateVersionInput{
		TenantID:  "payments",
		Key:       "payment.authorization.timeout",
		Value:     []byte(`1500`),
		CreatedBy: "test",
	})
	if err != nil {
		t.Fatalf("create candidate version: %v", err)
	}
	if _, err := registry.SetStableVersion(ctx, configregistry.SetStableVersionInput{
		TenantID:      "payments",
		Key:           "payment.authorization.timeout",
		Environment:   domain.EnvironmentProduction,
		VersionNumber: stable.VersionNumber,
	}); err != nil {
		t.Fatalf("set stable: %v", err)
	}
	return definition, stable, candidate
}

func registerAgent(t *testing.T, ctx context.Context, agents *agentregistry.Service, agentID string, serviceName string) {
	t.Helper()

	if _, err := agents.Register(ctx, agentregistry.RegisterInput{
		BootstrapToken: "bootstrap",
		ID:             agentID,
		Service:        serviceName,
		Environment:    domain.EnvironmentProduction,
		Instance:       agentID + "-instance",
	}); err != nil {
		t.Fatalf("register agent %s: %v", agentID, err)
	}
}

func threeDigits(value int) string {
	return string([]byte{
		byte('0' + value/100),
		byte('0' + value/10%10),
		byte('0' + value%10),
	})
}

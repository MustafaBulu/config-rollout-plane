package rollout

import (
	"context"
	"errors"
	"testing"
	"time"

	"config-rollout-plane/internal/agentregistry"
	"config-rollout-plane/internal/configregistry"
	"config-rollout-plane/internal/domain"
	"config-rollout-plane/internal/guardrail"
)

func TestServiceCreatesFrozenTargetsAndPromotesToStable(t *testing.T) {
	ctx := context.Background()
	registry, agents := setupRolloutDependencies(t, ctx, 500)
	service := NewService(NewMemoryStore(), registry, agents)

	created, targets, err := service.CreateRollout(ctx, CreateRolloutInput{
		TenantID:               "payments",
		Key:                    "payment.authorization.timeout",
		Environment:            domain.EnvironmentProduction,
		CandidateVersionNumber: 2,
		TargetServices:         []string{"payment-api"},
		Stages: []Stage{
			{ID: "stage-5", Percentage: 5},
			{ID: "stage-25", Percentage: 25},
			{ID: "stage-100", Percentage: 100},
		},
		RequiredAckPercentage: 100,
		DeploymentTimeout:     time.Minute,
	})
	if err != nil {
		t.Fatalf("create rollout: %v", err)
	}
	if created.State != StateDeploying {
		t.Fatalf("expected deploying rollout, got %s", created.State)
	}
	if created.CurrentStageID != "stage-5" {
		t.Fatalf("expected first stage, got %s", created.CurrentStageID)
	}
	if len(targets) == 0 {
		t.Fatal("expected frozen 5 percent targets")
	}
	for _, target := range targets {
		if !Eligible(target.Bucket, 5) {
			t.Fatalf("target bucket %d is outside 5 percent", target.Bucket)
		}
	}

	registerAgent(t, ctx, agents, "new-agent", "payment-api")
	active, activeTargets, err := service.ActiveRollout(ctx, created.ConfigDefinitionID, domain.EnvironmentProduction)
	if err != nil {
		t.Fatalf("active rollout: %v", err)
	}
	if active.CurrentStageID != "stage-5" {
		t.Fatalf("expected current stage to remain frozen, got %s", active.CurrentStageID)
	}
	if containsTarget(activeTargets, "new-agent") {
		t.Fatal("new agent joined the already frozen current stage")
	}

	rollout := acknowledgeCurrentStage(t, ctx, service, active, activeTargets)
	if rollout.CurrentStageID != "stage-25" {
		t.Fatalf("expected promotion to stage-25, got %s", rollout.CurrentStageID)
	}

	_, targets25, err := service.ActiveRollout(ctx, rollout.ConfigDefinitionID, domain.EnvironmentProduction)
	if err != nil {
		t.Fatalf("active rollout after promotion: %v", err)
	}
	if len(targets25) <= len(activeTargets) {
		t.Fatalf("expected 25 percent stage to have more targets than 5 percent: %d <= %d", len(targets25), len(activeTargets))
	}
	for _, target := range activeTargets {
		if !containsTarget(targets25, target.AgentID) {
			t.Fatalf("5 percent target %s missing from 25 percent target set", target.AgentID)
		}
	}

	rollout = acknowledgeCurrentStage(t, ctx, service, rollout, targets25)
	if rollout.CurrentStageID != "stage-100" {
		t.Fatalf("expected promotion to stage-100, got %s", rollout.CurrentStageID)
	}

	_, targets100, err := service.ActiveRollout(ctx, rollout.ConfigDefinitionID, domain.EnvironmentProduction)
	if err != nil {
		t.Fatalf("active rollout at 100 percent: %v", err)
	}
	rollout = acknowledgeCurrentStage(t, ctx, service, rollout, targets100)
	if rollout.State != StateCompleted {
		t.Fatalf("expected completed rollout, got %s", rollout.State)
	}

	state, err := registry.GetEnvironmentState(ctx, "payments", "payment.authorization.timeout", domain.EnvironmentProduction)
	if err != nil {
		t.Fatalf("get environment state: %v", err)
	}
	if state.StableVersionID != rollout.CandidateVersionID {
		t.Fatalf("expected candidate to become stable, got %q want %q", state.StableVersionID, rollout.CandidateVersionID)
	}
}

func TestServiceRollsBackWhenDeploymentTimeoutPassesWithoutCoverage(t *testing.T) {
	ctx := context.Background()
	registry, agents := setupRolloutDependencies(t, ctx, 200)
	store := NewMemoryStore()
	service := NewService(store, registry, agents)
	now := time.Date(2026, 8, 16, 10, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }

	created, targets, err := service.CreateRollout(ctx, CreateRolloutInput{
		TenantID:               "payments",
		Key:                    "payment.authorization.timeout",
		Environment:            domain.EnvironmentProduction,
		CandidateVersionNumber: 2,
		TargetServices:         []string{"payment-api"},
		Stages: []Stage{
			{ID: "stage-5", Percentage: 5},
			{ID: "stage-100", Percentage: 100},
		},
		RequiredAckPercentage: 100,
		DeploymentTimeout:     time.Minute,
	})
	if err != nil {
		t.Fatalf("create rollout: %v", err)
	}
	if len(targets) < 2 {
		t.Fatalf("expected at least two targets, got %d", len(targets))
	}

	now = now.Add(61 * time.Second)
	result, err := service.Acknowledge(ctx, AcknowledgeInput{
		RolloutID:        created.ID,
		StageID:          created.CurrentStageID,
		AgentID:          targets[0].AgentID,
		VersionID:        targets[0].ExpectedVersionID,
		SnapshotRevision: targets[0].SnapshotRevision,
	})
	if err != nil {
		t.Fatalf("acknowledge: %v", err)
	}
	if result.Decision != DecisionRollback {
		t.Fatalf("expected rollback decision, got %s", result.Decision)
	}
	if result.Rollout.State != StateRollingBack {
		t.Fatalf("expected rolling back state, got %s", result.Rollout.State)
	}
}

func TestServiceReconcileRollsBackTimedOutStageWithoutAcknowledgements(t *testing.T) {
	ctx := context.Background()
	registry, agents := setupRolloutDependencies(t, ctx, 200)
	service := NewService(NewMemoryStore(), registry, agents)
	now := time.Date(2026, 8, 16, 10, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }

	created, targets, err := service.CreateRollout(ctx, CreateRolloutInput{
		TenantID:               "payments",
		Key:                    "payment.authorization.timeout",
		Environment:            domain.EnvironmentProduction,
		CandidateVersionNumber: 2,
		TargetServices:         []string{"payment-api"},
		Stages: []Stage{
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
		t.Fatal("expected targets")
	}

	now = now.Add(61 * time.Second)
	if err := service.ReconcileActive(ctx); err != nil {
		t.Fatalf("reconcile active: %v", err)
	}

	got, _, _, err := service.GetRollout(ctx, created.ID)
	if err != nil {
		t.Fatalf("get rollout: %v", err)
	}
	if got.State != StateRollingBack {
		t.Fatalf("expected rolling back state, got %s", got.State)
	}
}

func TestServiceRollsBackWhenGuardrailBecomesUnhealthyAndVerifiesRollback(t *testing.T) {
	ctx := context.Background()
	registry, agents := setupRolloutDependencies(t, ctx, 200)
	service := NewService(NewMemoryStore(), registry, agents)
	service.SetGuardrailQueryer(staticQueryer{value: 0.05})

	created, targets, err := service.CreateRollout(ctx, CreateRolloutInput{
		TenantID:               "payments",
		Key:                    "payment.authorization.timeout",
		Environment:            domain.EnvironmentProduction,
		CandidateVersionNumber: 2,
		TargetServices:         []string{"payment-api"},
		Stages: []Stage{
			{ID: "stage-5", Percentage: 5},
			{ID: "stage-100", Percentage: 100},
		},
		Guardrails: []guardrail.Rule{
			{
				Name:                "error-rate",
				Query:               `sum(rate(payment_requests_total{result="error"}[1m]))`,
				Operator:            guardrail.OperatorLessThan,
				Threshold:           0.02,
				ConsecutiveFailures: 1,
			},
		},
		RequiredAckPercentage: 100,
		DeploymentTimeout:     time.Minute,
		RolloutMaxDuration:    15 * time.Minute,
		RollbackTimeout:       time.Minute,
	})
	if err != nil {
		t.Fatalf("create rollout: %v", err)
	}
	if len(targets) == 0 {
		t.Fatal("expected targets")
	}

	rolledBack := acknowledgeCurrentStage(t, ctx, service, created, targets)
	if rolledBack.State != StateRollingBack {
		t.Fatalf("expected rolling back state, got %s", rolledBack.State)
	}

	if err := service.ReconcileActive(ctx); err != nil {
		t.Fatalf("activate rollback verification: %v", err)
	}
	verifying, verificationTargets, _, err := service.GetRollout(ctx, created.ID)
	if err != nil {
		t.Fatalf("get verifying rollout: %v", err)
	}
	if verifying.CurrentStageID != rollbackVerificationStageID(created.ID) {
		t.Fatalf("expected rollback verification stage, got %s", verifying.CurrentStageID)
	}
	if len(verificationTargets) != len(targets) {
		t.Fatalf("expected rollback verification targets to match candidate targets: %d != %d", len(verificationTargets), len(targets))
	}
	for _, target := range verificationTargets {
		if target.ExpectedVersionID != verifying.StableVersionID {
			t.Fatalf("rollback verification should expect stable version, got %q", target.ExpectedVersionID)
		}
	}

	final := acknowledgeCurrentStage(t, ctx, service, verifying, verificationTargets)
	if final.State != StateRolledBack {
		t.Fatalf("expected rolled back state, got %s", final.State)
	}
	if final.RollbackStatus != RollbackStatusVerified {
		t.Fatalf("expected verified rollback, got %s", final.RollbackStatus)
	}

	state, err := registry.GetEnvironmentState(ctx, "payments", "payment.authorization.timeout", domain.EnvironmentProduction)
	if err != nil {
		t.Fatalf("get environment state: %v", err)
	}
	if state.StableVersionID != final.StableVersionID {
		t.Fatalf("rollback must keep previous stable version, got %q want %q", state.StableVersionID, final.StableVersionID)
	}
}

func TestServicePausesOnUnknownGuardrailAndMaxDurationForcesRollback(t *testing.T) {
	ctx := context.Background()
	registry, agents := setupRolloutDependencies(t, ctx, 200)
	service := NewService(NewMemoryStore(), registry, agents)
	service.SetGuardrailQueryer(staticQueryer{err: errors.New("prometheus unavailable")})
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }

	created, targets, err := service.CreateRollout(ctx, CreateRolloutInput{
		TenantID:               "payments",
		Key:                    "payment.authorization.timeout",
		Environment:            domain.EnvironmentProduction,
		CandidateVersionNumber: 2,
		TargetServices:         []string{"payment-api"},
		Stages: []Stage{
			{ID: "stage-5", Percentage: 5},
			{ID: "stage-100", Percentage: 100},
		},
		Guardrails: []guardrail.Rule{
			{Name: "error-rate", Query: "up", Operator: guardrail.OperatorLessThan, Threshold: 0.02},
		},
		RequiredAckPercentage: 100,
		DeploymentTimeout:     time.Minute,
		RolloutMaxDuration:    2 * time.Minute,
		RollbackTimeout:       time.Minute,
	})
	if err != nil {
		t.Fatalf("create rollout: %v", err)
	}

	paused := acknowledgeCurrentStage(t, ctx, service, created, targets)
	if paused.State != StatePaused {
		t.Fatalf("expected paused state for unknown guardrail, got %s", paused.State)
	}
	if paused.CurrentStageID != created.CurrentStageID {
		t.Fatalf("unknown guardrail should not promote, got stage %s", paused.CurrentStageID)
	}

	now = now.Add(2*time.Minute + time.Second)
	if err := service.ReconcileActive(ctx); err != nil {
		t.Fatalf("reconcile max duration: %v", err)
	}
	got, _, _, err := service.GetRollout(ctx, created.ID)
	if err != nil {
		t.Fatalf("get rollout: %v", err)
	}
	if got.State != StateRollingBack {
		t.Fatalf("expected rolling back after max duration, got %s", got.State)
	}
}

func TestServiceMarksRollbackPartialWhenVerificationTimesOut(t *testing.T) {
	ctx := context.Background()
	registry, agents := setupRolloutDependencies(t, ctx, 200)
	service := NewService(NewMemoryStore(), registry, agents)
	service.SetGuardrailQueryer(staticQueryer{value: 0.05})
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }

	created, targets, err := service.CreateRollout(ctx, CreateRolloutInput{
		TenantID:               "payments",
		Key:                    "payment.authorization.timeout",
		Environment:            domain.EnvironmentProduction,
		CandidateVersionNumber: 2,
		TargetServices:         []string{"payment-api"},
		Stages: []Stage{
			{ID: "stage-5", Percentage: 5},
			{ID: "stage-100", Percentage: 100},
		},
		Guardrails: []guardrail.Rule{
			{Name: "error-rate", Query: "up", Operator: guardrail.OperatorLessThan, Threshold: 0.02},
		},
		RequiredAckPercentage: 100,
		DeploymentTimeout:     time.Minute,
		RolloutMaxDuration:    15 * time.Minute,
		RollbackTimeout:       time.Minute,
	})
	if err != nil {
		t.Fatalf("create rollout: %v", err)
	}
	rolledBack := acknowledgeCurrentStage(t, ctx, service, created, targets)
	if rolledBack.State != StateRollingBack {
		t.Fatalf("expected rolling back state, got %s", rolledBack.State)
	}

	if err := service.ReconcileActive(ctx); err != nil {
		t.Fatalf("activate rollback verification: %v", err)
	}
	now = now.Add(time.Minute + time.Second)
	if err := service.ReconcileActive(ctx); err != nil {
		t.Fatalf("reconcile rollback timeout: %v", err)
	}

	got, _, _, err := service.GetRollout(ctx, created.ID)
	if err != nil {
		t.Fatalf("get rollout: %v", err)
	}
	if got.State != StateRolledBack {
		t.Fatalf("expected rolled back state, got %s", got.State)
	}
	if got.RollbackStatus != RollbackStatusPartial {
		t.Fatalf("expected partial rollback, got %s", got.RollbackStatus)
	}
}

func setupRolloutDependencies(t *testing.T, ctx context.Context, agentCount int) (*configregistry.Service, *agentregistry.Service) {
	t.Helper()

	registry := configregistry.NewService(configregistry.NewMemoryStore(), configregistry.JSONSchemaValidator{})
	if _, err := registry.CreateTenant(ctx, configregistry.CreateTenantInput{ID: "payments", Name: "Payments"}); err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	if _, err := registry.CreateDefinition(ctx, configregistry.CreateDefinitionInput{
		TenantID:     "payments",
		Key:          "payment.authorization.timeout",
		Schema:       []byte(`{"type":"integer","minimum":100,"maximum":10000}`),
		DefaultValue: []byte(`2000`),
	}); err != nil {
		t.Fatalf("create definition: %v", err)
	}
	if _, err := registry.CreateVersion(ctx, configregistry.CreateVersionInput{
		TenantID:  "payments",
		Key:       "payment.authorization.timeout",
		Value:     []byte(`2000`),
		CreatedBy: "test",
	}); err != nil {
		t.Fatalf("create v1: %v", err)
	}
	if _, err := registry.CreateVersion(ctx, configregistry.CreateVersionInput{
		TenantID:  "payments",
		Key:       "payment.authorization.timeout",
		Value:     []byte(`1500`),
		CreatedBy: "test",
	}); err != nil {
		t.Fatalf("create v2: %v", err)
	}
	if _, err := registry.SetStableVersion(ctx, configregistry.SetStableVersionInput{
		TenantID:      "payments",
		Key:           "payment.authorization.timeout",
		Environment:   domain.EnvironmentProduction,
		VersionNumber: 1,
	}); err != nil {
		t.Fatalf("set stable v1: %v", err)
	}

	agents := agentregistry.NewService(agentregistry.NewMemoryStore(), "bootstrap", time.Hour)
	for i := range agentCount {
		registerAgent(t, ctx, agents, "agent-"+threeDigits(i), "payment-api")
	}
	registerAgent(t, ctx, agents, "other-service-agent", "recommendation-api")
	return registry, agents
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

func acknowledgeCurrentStage(t *testing.T, ctx context.Context, service *Service, rollout Rollout, targets []StageTarget) Rollout {
	t.Helper()

	current := rollout
	for _, target := range targets {
		result, err := service.Acknowledge(ctx, AcknowledgeInput{
			RolloutID:        current.ID,
			StageID:          current.CurrentStageID,
			AgentID:          target.AgentID,
			VersionID:        target.ExpectedVersionID,
			SnapshotRevision: target.SnapshotRevision,
		})
		if err != nil {
			t.Fatalf("ack target %s: %v", target.AgentID, err)
		}
		current = result.Rollout
	}
	return current
}

type staticQueryer struct {
	value float64
	err   error
}

func (q staticQueryer) Query(ctx context.Context, query string, at time.Time) (float64, error) {
	return q.value, q.err
}

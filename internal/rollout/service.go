package rollout

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"slices"
	"time"

	"config-rollout-plane/internal/configregistry"
	"config-rollout-plane/internal/domain"
)

type Store interface {
	CreateRollout(ctx context.Context, rollout Rollout, stages []Stage) error
	GetRollout(ctx context.Context, rolloutID string) (Rollout, error)
	GetActiveRollout(ctx context.Context, configDefinitionID string, environment string) (Rollout, error)
	ListActiveRollouts(ctx context.Context) ([]Rollout, error)
	UpdateRollout(ctx context.Context, rollout Rollout) error
	ListStages(ctx context.Context, rolloutID string) ([]Stage, error)
	SaveStageTargets(ctx context.Context, targets []StageTarget) error
	ReplaceStageTargets(ctx context.Context, rolloutID string, stageID string, targets []StageTarget) error
	ListStageTargets(ctx context.Context, rolloutID string, stageID string) ([]StageTarget, error)
	NextSnapshotRevision(ctx context.Context) (int64, error)
}

type Registry interface {
	GetDefinition(ctx context.Context, tenantID string, key string) (domain.ConfigDefinition, error)
	ListVersions(ctx context.Context, tenantID string, key string) ([]domain.ConfigVersion, error)
	GetEnvironmentState(ctx context.Context, tenantID string, key string, environment domain.Environment) (domain.ConfigEnvironmentState, error)
	SetStableVersion(ctx context.Context, input configregistry.SetStableVersionInput) (domain.ConfigEnvironmentState, error)
}

type AgentSource interface {
	ListAgents(ctx context.Context) ([]domain.Agent, error)
}

type Service struct {
	store     Store
	registry  Registry
	agents    AgentSource
	now       func() time.Time
	newID     func(prefix string) string
	stagePlan []Stage
}

func NewService(store Store, registry Registry, agents AgentSource) *Service {
	return &Service{
		store:     store,
		registry:  registry,
		agents:    agents,
		now:       func() time.Time { return time.Now().UTC() },
		newID:     randomID,
		stagePlan: DefaultStagePlan(),
	}
}

type CreateRolloutInput struct {
	TenantID               string
	Key                    string
	Environment            domain.Environment
	CandidateVersionNumber int
	TargetServices         []string
	Stages                 []Stage
	RequiredAckPercentage  float64
	DeploymentTimeout      time.Duration
}

func (s *Service) CreateRollout(ctx context.Context, input CreateRolloutInput) (Rollout, []StageTarget, error) {
	if input.TenantID == "" || input.Key == "" {
		return Rollout{}, nil, fmt.Errorf("%w: tenant and key are required", ErrInvalidInput)
	}
	if err := input.Environment.Validate(); err != nil {
		return Rollout{}, nil, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	if input.CandidateVersionNumber < 1 {
		return Rollout{}, nil, fmt.Errorf("%w: candidate version number must be positive", ErrInvalidInput)
	}

	stages := input.Stages
	if len(stages) == 0 {
		stages = s.stagePlan
	}
	if err := ValidateStagePlan(stages); err != nil {
		return Rollout{}, nil, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}

	definition, err := s.registry.GetDefinition(ctx, input.TenantID, input.Key)
	if err != nil {
		return Rollout{}, nil, err
	}
	versions, err := s.registry.ListVersions(ctx, input.TenantID, input.Key)
	if err != nil {
		return Rollout{}, nil, err
	}
	candidate, ok := findVersionByNumber(versions, input.CandidateVersionNumber)
	if !ok {
		return Rollout{}, nil, fmt.Errorf("%w: candidate version", ErrNotFound)
	}
	state, err := s.registry.GetEnvironmentState(ctx, input.TenantID, input.Key, input.Environment)
	if err != nil {
		return Rollout{}, nil, err
	}
	if state.StableVersionID == "" {
		return Rollout{}, nil, fmt.Errorf("%w: stable version is required", ErrInvalidInput)
	}
	if state.StableVersionID == candidate.ID {
		return Rollout{}, nil, fmt.Errorf("%w: candidate is already stable", ErrConflict)
	}

	requiredAck := input.RequiredAckPercentage
	if requiredAck == 0 {
		requiredAck = 95
	}
	deploymentTimeout := input.DeploymentTimeout
	if deploymentTimeout == 0 {
		deploymentTimeout = 90 * time.Second
	}

	now := s.now()
	rollout := Rollout{
		ID:                 s.newID("rollout"),
		ConfigDefinitionID: definition.ID,
		TenantID:           input.TenantID,
		ConfigKey:          input.Key,
		Environment:        input.Environment,
		TargetServices:     append([]string(nil), input.TargetServices...),
		StableVersionID:    state.StableVersionID,
		CandidateVersionID: candidate.ID,
		CandidateVersion:   candidate.VersionNumber,
		State:              StateDeploying,
		CurrentStageID:     stages[0].ID,
		CurrentStageIndex:  0,
		RequiredAckPercent: requiredAck,
		StageStartedAt:     now,
		DeploymentTimeout:  deploymentTimeout,
		CreatedAt:          now,
		UpdatedAt:          now,
	}

	if err := s.store.CreateRollout(ctx, rollout, stages); err != nil {
		return Rollout{}, nil, err
	}

	targets, err := s.activateStage(ctx, rollout, stages[0], input.TargetServices)
	if err != nil {
		return Rollout{}, nil, err
	}
	return rollout, targets, nil
}

func (s *Service) GetRollout(ctx context.Context, rolloutID string) (Rollout, []StageTarget, Coverage, error) {
	rollout, err := s.store.GetRollout(ctx, rolloutID)
	if err != nil {
		return Rollout{}, nil, Coverage{}, err
	}
	targets, err := s.store.ListStageTargets(ctx, rollout.ID, rollout.CurrentStageID)
	if err != nil {
		return Rollout{}, nil, Coverage{}, err
	}
	return rollout, targets, AcknowledgementCoverage(targets), nil
}

type AcknowledgeInput struct {
	RolloutID        string
	StageID          string
	AgentID          string
	VersionID        string
	SnapshotRevision int64
}

type AcknowledgeResult struct {
	Counted  bool
	Coverage Coverage
	Decision StageDecision
	Rollout  Rollout
}

func (s *Service) Acknowledge(ctx context.Context, input AcknowledgeInput) (AcknowledgeResult, error) {
	rollout, err := s.store.GetRollout(ctx, input.RolloutID)
	if err != nil {
		return AcknowledgeResult{}, err
	}
	if rollout.State.Terminal() {
		return AcknowledgeResult{}, fmt.Errorf("%w: rollout is terminal", ErrConflict)
	}

	targets, err := s.store.ListStageTargets(ctx, rollout.ID, rollout.CurrentStageID)
	if err != nil {
		return AcknowledgeResult{}, err
	}

	counted, err := ApplyAck(targets, Ack{
		RolloutID:        input.RolloutID,
		StageID:          input.StageID,
		AgentID:          input.AgentID,
		VersionID:        input.VersionID,
		SnapshotRevision: input.SnapshotRevision,
		AckedAt:          s.now(),
	})
	if err != nil {
		return AcknowledgeResult{}, err
	}
	if counted {
		if err := s.store.ReplaceStageTargets(ctx, rollout.ID, rollout.CurrentStageID, targets); err != nil {
			return AcknowledgeResult{}, err
		}
	}

	stages, err := s.store.ListStages(ctx, rollout.ID)
	if err != nil {
		return AcknowledgeResult{}, err
	}
	currentStage := stages[rollout.CurrentStageIndex]
	decision := EvaluateStageProgress(StageProgressInput{
		Targets:               targets,
		RequiredAckPercentage: rollout.RequiredAckPercent,
		StageStartedAt:        rollout.StageStartedAt,
		Now:                   s.now(),
		MinimumDuration:       currentStage.MinimumDuration,
		DeploymentTimeout:     rollout.DeploymentTimeout,
	})

	switch decision {
	case DecisionPromote:
		rollout, err = s.promote(ctx, rollout, stages)
	case DecisionRollback:
		rollout, err = s.transition(ctx, rollout, StateRollingBack)
	}
	if err != nil {
		return AcknowledgeResult{}, err
	}

	return AcknowledgeResult{
		Counted:  counted,
		Coverage: AcknowledgementCoverage(targets),
		Decision: decision,
		Rollout:  rollout,
	}, nil
}

func (s *Service) ActiveRollout(ctx context.Context, configDefinitionID string, environment domain.Environment) (Rollout, []StageTarget, error) {
	rollout, err := s.store.GetActiveRollout(ctx, configDefinitionID, string(environment))
	if err != nil {
		return Rollout{}, nil, err
	}
	targets, err := s.store.ListStageTargets(ctx, rollout.ID, rollout.CurrentStageID)
	if err != nil {
		return Rollout{}, nil, err
	}
	return rollout, targets, nil
}

func (s *Service) ReconcileActive(ctx context.Context) error {
	active, err := s.store.ListActiveRollouts(ctx)
	if err != nil {
		return err
	}

	for _, rollout := range active {
		if rollout.State.Terminal() || rollout.State == StateRollingBack {
			continue
		}
		if _, err := s.reconcileRollout(ctx, rollout); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) reconcileRollout(ctx context.Context, rollout Rollout) (Rollout, error) {
	targets, err := s.store.ListStageTargets(ctx, rollout.ID, rollout.CurrentStageID)
	if err != nil {
		return Rollout{}, err
	}
	stages, err := s.store.ListStages(ctx, rollout.ID)
	if err != nil {
		return Rollout{}, err
	}
	if rollout.CurrentStageIndex < 0 || rollout.CurrentStageIndex >= len(stages) {
		return Rollout{}, fmt.Errorf("%w: current stage index out of range", ErrInvalidInput)
	}

	currentStage := stages[rollout.CurrentStageIndex]
	decision := EvaluateStageProgress(StageProgressInput{
		Targets:               targets,
		RequiredAckPercentage: rollout.RequiredAckPercent,
		StageStartedAt:        rollout.StageStartedAt,
		Now:                   s.now(),
		MinimumDuration:       currentStage.MinimumDuration,
		DeploymentTimeout:     rollout.DeploymentTimeout,
	})

	switch decision {
	case DecisionPromote:
		return s.promote(ctx, rollout, stages)
	case DecisionRollback:
		return s.transition(ctx, rollout, StateRollingBack)
	default:
		return rollout, nil
	}
}

func (s *Service) promote(ctx context.Context, rollout Rollout, stages []Stage) (Rollout, error) {
	if rollout.CurrentStageIndex == len(stages)-1 {
		if err := Transition(rollout.State, StateEvaluating); err != nil && rollout.State != StateEvaluating {
			return Rollout{}, err
		}
		if _, err := s.registry.SetStableVersion(ctx, configregistry.SetStableVersionInput{
			TenantID:      rollout.TenantID,
			Key:           rollout.ConfigKey,
			Environment:   rollout.Environment,
			VersionNumber: rollout.CandidateVersion,
		}); err != nil {
			return Rollout{}, err
		}
		rollout.State = StateCompleted
		rollout.UpdatedAt = s.now()
		return rollout, s.store.UpdateRollout(ctx, rollout)
	}

	nextIndex := rollout.CurrentStageIndex + 1
	nextStage := stages[nextIndex]
	rollout.State = StateDeploying
	rollout.CurrentStageIndex = nextIndex
	rollout.CurrentStageID = nextStage.ID
	rollout.StageStartedAt = s.now()
	rollout.UpdatedAt = rollout.StageStartedAt

	if err := s.store.UpdateRollout(ctx, rollout); err != nil {
		return Rollout{}, err
	}
	if _, err := s.activateStage(ctx, rollout, nextStage, rollout.TargetServices); err != nil {
		return Rollout{}, err
	}
	return rollout, nil
}

func (s *Service) transition(ctx context.Context, rollout Rollout, next State) (Rollout, error) {
	if err := Transition(rollout.State, next); err != nil {
		return Rollout{}, err
	}
	rollout.State = next
	rollout.UpdatedAt = s.now()
	if err := s.store.UpdateRollout(ctx, rollout); err != nil {
		return Rollout{}, err
	}
	return rollout, nil
}

func (s *Service) activateStage(ctx context.Context, rollout Rollout, stage Stage, targetServices []string) ([]StageTarget, error) {
	agents, err := s.agents.ListAgents(ctx)
	if err != nil {
		return nil, err
	}
	agents = filterAgents(agents, rollout.Environment, targetServices)

	revision, err := s.store.NextSnapshotRevision(ctx)
	if err != nil {
		return nil, err
	}
	targets, err := BuildStageTargets(BuildTargetsInput{
		Rollout:          rollout,
		Stage:            stage,
		Agents:           agents,
		SnapshotRevision: revision,
		CreatedAt:        s.now(),
	})
	if err != nil {
		return nil, err
	}
	if err := s.store.SaveStageTargets(ctx, targets); err != nil {
		return nil, err
	}
	return targets, nil
}

func filterAgents(agents []domain.Agent, environment domain.Environment, services []string) []domain.Agent {
	filtered := make([]domain.Agent, 0, len(agents))
	for _, agent := range agents {
		if agent.Environment != environment {
			continue
		}
		if len(services) > 0 && !slices.Contains(services, agent.Service) {
			continue
		}
		filtered = append(filtered, agent)
	}
	return filtered
}

func findVersionByNumber(versions []domain.ConfigVersion, number int) (domain.ConfigVersion, bool) {
	for _, version := range versions {
		if version.VersionNumber == number {
			return version, true
		}
	}
	return domain.ConfigVersion{}, false
}

func randomID(prefix string) string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return prefix + "_" + hex.EncodeToString(b[:])
}

package rollout

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"slices"
	"sync"
	"time"

	"config-rollout-plane/internal/configregistry"
	"config-rollout-plane/internal/domain"
	"config-rollout-plane/internal/guardrail"
)

type Store interface {
	CreateRollout(ctx context.Context, rollout Rollout, stages []Stage) error
	GetRollout(ctx context.Context, rolloutID string) (Rollout, error)
	GetActiveRollout(ctx context.Context, configDefinitionID string, environment string) (Rollout, error)
	ListActiveRollouts(ctx context.Context) ([]Rollout, error)
	UpdateRollout(ctx context.Context, rollout Rollout) error
	ListStages(ctx context.Context, rolloutID string) ([]Stage, error)
	SaveStage(ctx context.Context, rolloutID string, stage Stage) error
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
	mu        sync.Mutex
	store     Store
	registry  Registry
	agents    AgentSource
	now       func() time.Time
	newID     func(prefix string) string
	stagePlan []Stage
	queryer   guardrail.Queryer
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
	Guardrails             []guardrail.Rule
	RequiredAckPercentage  float64
	DeploymentTimeout      time.Duration
	RolloutMaxDuration     time.Duration
	RollbackTimeout        time.Duration
}

func (s *Service) SetGuardrailQueryer(queryer guardrail.Queryer) {
	s.queryer = queryer
}

func (s *Service) CreateRollout(ctx context.Context, input CreateRolloutInput) (Rollout, []StageTarget, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

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
	for _, rule := range input.Guardrails {
		if err := rule.Validate(); err != nil {
			return Rollout{}, nil, err
		}
		if rule.Query == "" {
			return Rollout{}, nil, fmt.Errorf("%w: guardrail query is required", ErrInvalidInput)
		}
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
	rolloutMaxDuration := input.RolloutMaxDuration
	if rolloutMaxDuration == 0 {
		rolloutMaxDuration = 15 * time.Minute
	}
	rollbackTimeout := input.RollbackTimeout
	if rollbackTimeout == 0 {
		rollbackTimeout = 2 * time.Minute
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
		Guardrails:         append([]guardrail.Rule(nil), input.Guardrails...),
		GuardrailFailures:  map[string]int{},
		StageStartedAt:     now,
		DeploymentTimeout:  deploymentTimeout,
		RolloutMaxDuration: rolloutMaxDuration,
		RollbackTimeout:    rollbackTimeout,
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
	s.mu.Lock()
	defer s.mu.Unlock()

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
	if rollout.State == StateRollingBack {
		rollout, err = s.reconcileRollback(ctx, rollout, targets)
		if err != nil {
			return AcknowledgeResult{}, err
		}
		return AcknowledgeResult{
			Counted:  counted,
			Coverage: AcknowledgementCoverage(targets),
			Decision: DecisionWait,
			Rollout:  rollout,
		}, nil
	}
	if rollout.CurrentStageIndex < 0 || rollout.CurrentStageIndex >= len(stages) {
		return AcknowledgeResult{}, fmt.Errorf("%w: current stage index out of range", ErrInvalidInput)
	}

	currentStage := stages[rollout.CurrentStageIndex]
	decision, evaluated, err := s.evaluateStageDecision(ctx, rollout, targets, currentStage)
	if err != nil {
		return AcknowledgeResult{}, err
	}
	rollout = evaluated

	switch decision {
	case DecisionPromote:
		rollout, err = s.promote(ctx, rollout, stages)
	case DecisionRollback:
		rollout, err = s.startRollback(ctx, rollout)
	case DecisionPause:
		if rollout.State != StatePaused {
			rollout, err = s.transition(ctx, rollout, StatePaused)
		}
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
	s.mu.Lock()
	defer s.mu.Unlock()

	active, err := s.store.ListActiveRollouts(ctx)
	if err != nil {
		return err
	}

	for _, rollout := range active {
		if rollout.State.Terminal() {
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
	if rollout.State == StateRollingBack {
		return s.reconcileRollback(ctx, rollout, targets)
	}

	stages, err := s.store.ListStages(ctx, rollout.ID)
	if err != nil {
		return Rollout{}, err
	}
	if rollout.CurrentStageIndex < 0 || rollout.CurrentStageIndex >= len(stages) {
		return Rollout{}, fmt.Errorf("%w: current stage index out of range", ErrInvalidInput)
	}

	currentStage := stages[rollout.CurrentStageIndex]
	decision, evaluated, err := s.evaluateStageDecision(ctx, rollout, targets, currentStage)
	if err != nil {
		return Rollout{}, err
	}
	rollout = evaluated

	switch decision {
	case DecisionPromote:
		return s.promote(ctx, rollout, stages)
	case DecisionRollback:
		return s.startRollback(ctx, rollout)
	case DecisionPause:
		if rollout.State == StatePaused {
			return rollout, nil
		}
		return s.transition(ctx, rollout, StatePaused)
	default:
		return rollout, nil
	}
}

func (s *Service) evaluateStageDecision(ctx context.Context, rollout Rollout, targets []StageTarget, currentStage Stage) (StageDecision, Rollout, error) {
	now := s.now()
	if rollout.RolloutMaxDuration > 0 && now.Sub(rollout.CreatedAt) >= rollout.RolloutMaxDuration {
		return DecisionRollback, rollout, nil
	}

	decision := EvaluateStageProgress(StageProgressInput{
		Targets:               targets,
		RequiredAckPercentage: rollout.RequiredAckPercent,
		StageStartedAt:        rollout.StageStartedAt,
		Now:                   now,
		MinimumDuration:       currentStage.MinimumDuration,
		DeploymentTimeout:     rollout.DeploymentTimeout,
	})
	if decision != DecisionPromote {
		return decision, rollout, nil
	}

	if len(rollout.Guardrails) == 0 {
		return DecisionPromote, rollout, nil
	}

	status, evaluated, err := s.evaluateGuardrails(ctx, rollout, now)
	if err != nil {
		return "", Rollout{}, err
	}
	rollout = evaluated

	switch status {
	case guardrail.StatusHealthy:
		return DecisionPromote, rollout, nil
	case guardrail.StatusUnhealthy:
		return DecisionRollback, rollout, nil
	default:
		return DecisionPause, rollout, nil
	}
}

func (s *Service) evaluateGuardrails(ctx context.Context, rollout Rollout, observedAt time.Time) (guardrail.Status, Rollout, error) {
	if rollout.GuardrailFailures == nil {
		rollout.GuardrailFailures = map[string]int{}
	}

	evaluations := make([]guardrail.Evaluation, 0, len(rollout.Guardrails))
	for _, rule := range rollout.Guardrails {
		previousFailures := rollout.GuardrailFailures[rule.Name]
		var evaluation guardrail.Evaluation
		var err error
		if s.queryer == nil {
			evaluation, err = guardrail.Unknown(rule, previousFailures, "prometheus queryer is not configured", observedAt)
		} else {
			value, queryErr := s.queryer.Query(ctx, rule.Query, observedAt)
			evaluation, err = guardrail.EvaluateQueryResult(rule, value, queryErr, previousFailures, observedAt)
		}
		if err != nil {
			return "", Rollout{}, err
		}
		rollout.GuardrailFailures[rule.Name] = evaluation.ConsecutiveFailures
		evaluations = append(evaluations, evaluation)
	}

	rollout.UpdatedAt = observedAt
	if err := s.store.UpdateRollout(ctx, rollout); err != nil {
		return "", Rollout{}, err
	}
	return guardrail.AggregateStatus(evaluations), rollout, nil
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

func rollbackVerificationStageID(rolloutID string) string {
	return rolloutID + "-rollback-verification"
}

func (s *Service) startRollback(ctx context.Context, rollout Rollout) (Rollout, error) {
	if rollout.State == StateRollingBack {
		return rollout, nil
	}
	return s.transition(ctx, rollout, StateRollingBack)
}

func (s *Service) reconcileRollback(ctx context.Context, rollout Rollout, targets []StageTarget) (Rollout, error) {
	verificationStageID := rollbackVerificationStageID(rollout.ID)
	if rollout.CurrentStageID != verificationStageID {
		return s.activateRollbackVerification(ctx, rollout, targets)
	}

	if CoverageReached(targets, rollout.RequiredAckPercent) || len(targets) == 0 {
		rollout.State = StateRolledBack
		rollout.RollbackStatus = RollbackStatusVerified
		rollout.UpdatedAt = s.now()
		return rollout, s.store.UpdateRollout(ctx, rollout)
	}

	if rollout.RollbackTimeout > 0 && s.now().Sub(rollout.StageStartedAt) >= rollout.RollbackTimeout {
		rollout.State = StateRolledBack
		rollout.RollbackStatus = RollbackStatusPartial
		rollout.UpdatedAt = s.now()
		return rollout, s.store.UpdateRollout(ctx, rollout)
	}

	return rollout, nil
}

func (s *Service) activateRollbackVerification(ctx context.Context, rollout Rollout, candidateTargets []StageTarget) (Rollout, error) {
	revision, err := s.store.NextSnapshotRevision(ctx)
	if err != nil {
		return Rollout{}, err
	}

	now := s.now()
	verificationStageID := rollbackVerificationStageID(rollout.ID)
	rollbackTargets := make([]StageTarget, 0, len(candidateTargets))
	for _, target := range candidateTargets {
		rollbackTargets = append(rollbackTargets, StageTarget{
			RolloutID:         rollout.ID,
			StageID:           verificationStageID,
			AgentID:           target.AgentID,
			Bucket:            target.Bucket,
			ExpectedVersionID: rollout.StableVersionID,
			SnapshotRevision:  revision,
			CreatedAt:         now,
			Status:            TargetPending,
		})
	}

	if err := s.store.SaveStage(ctx, rollout.ID, Stage{ID: verificationStageID, Percentage: 100}); err != nil {
		return Rollout{}, err
	}
	if err := s.store.SaveStageTargets(ctx, rollbackTargets); err != nil {
		return Rollout{}, err
	}
	rollout.CurrentStageID = verificationStageID
	rollout.StageStartedAt = now
	rollout.UpdatedAt = now
	if err := s.store.UpdateRollout(ctx, rollout); err != nil {
		return Rollout{}, err
	}
	if len(rollbackTargets) == 0 {
		rollout.State = StateRolledBack
		rollout.RollbackStatus = RollbackStatusVerified
		rollout.UpdatedAt = s.now()
		return rollout, s.store.UpdateRollout(ctx, rollout)
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

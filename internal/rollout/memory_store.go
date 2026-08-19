package rollout

import (
	"context"
	"fmt"
	"sync"

	"config-rollout-plane/internal/guardrail"
)

type MemoryStore struct {
	mu sync.RWMutex

	rollouts      map[string]Rollout
	activeRollout map[string]string
	stages        map[string][]Stage
	targets       map[string][]StageTarget
	nextRevision  int64
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		rollouts:      make(map[string]Rollout),
		activeRollout: make(map[string]string),
		stages:        make(map[string][]Stage),
		targets:       make(map[string][]StageTarget),
		nextRevision:  1,
	}
}

func (s *MemoryStore) CreateRollout(ctx context.Context, rollout Rollout, stages []Stage) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.rollouts[rollout.ID]; ok {
		return fmt.Errorf("%w: rollout %q", ErrAlreadyExists, rollout.ID)
	}

	activeKey := rolloutActiveKey(rollout.ConfigDefinitionID, string(rollout.Environment))
	if existingID, ok := s.activeRollout[activeKey]; ok {
		if existing := s.rollouts[existingID]; !existing.State.Terminal() {
			return fmt.Errorf("%w: active rollout exists", ErrConflict)
		}
	}

	s.rollouts[rollout.ID] = cloneRollout(rollout)
	s.stages[rollout.ID] = append([]Stage(nil), stages...)
	if !rollout.State.Terminal() {
		s.activeRollout[activeKey] = rollout.ID
	}
	return nil
}

func (s *MemoryStore) GetRollout(ctx context.Context, rolloutID string) (Rollout, error) {
	if err := ctx.Err(); err != nil {
		return Rollout{}, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	rollout, ok := s.rollouts[rolloutID]
	if !ok {
		return Rollout{}, fmt.Errorf("%w: rollout %q", ErrNotFound, rolloutID)
	}
	return cloneRollout(rollout), nil
}

func (s *MemoryStore) GetActiveRollout(ctx context.Context, configDefinitionID string, environment string) (Rollout, error) {
	if err := ctx.Err(); err != nil {
		return Rollout{}, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	rolloutID, ok := s.activeRollout[configDefinitionID+"\x00"+environment]
	if !ok {
		return Rollout{}, fmt.Errorf("%w: active rollout", ErrNotFound)
	}
	rollout := s.rollouts[rolloutID]
	if rollout.State.Terminal() {
		return Rollout{}, fmt.Errorf("%w: active rollout", ErrNotFound)
	}
	return cloneRollout(rollout), nil
}

func (s *MemoryStore) ListActiveRollouts(ctx context.Context) ([]Rollout, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	rollouts := make([]Rollout, 0, len(s.activeRollout))
	for _, rolloutID := range s.activeRollout {
		rollout := s.rollouts[rolloutID]
		if rollout.State.Terminal() {
			continue
		}
		rollouts = append(rollouts, cloneRollout(rollout))
	}
	return rollouts, nil
}

func (s *MemoryStore) UpdateRollout(ctx context.Context, rollout Rollout) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.rollouts[rollout.ID]; !ok {
		return fmt.Errorf("%w: rollout %q", ErrNotFound, rollout.ID)
	}

	s.rollouts[rollout.ID] = cloneRollout(rollout)
	activeKey := rolloutActiveKey(rollout.ConfigDefinitionID, string(rollout.Environment))
	if rollout.State.Terminal() {
		delete(s.activeRollout, activeKey)
	} else {
		s.activeRollout[activeKey] = rollout.ID
	}
	return nil
}

func (s *MemoryStore) ListStages(ctx context.Context, rolloutID string) ([]Stage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	stages, ok := s.stages[rolloutID]
	if !ok {
		return nil, fmt.Errorf("%w: stages for rollout %q", ErrNotFound, rolloutID)
	}
	return append([]Stage(nil), stages...), nil
}

func (s *MemoryStore) SaveStage(ctx context.Context, rolloutID string, stage Stage) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.rollouts[rolloutID]; !ok {
		return fmt.Errorf("%w: rollout %q", ErrNotFound, rolloutID)
	}
	for _, existing := range s.stages[rolloutID] {
		if existing.ID == stage.ID {
			return nil
		}
	}
	s.stages[rolloutID] = append(s.stages[rolloutID], stage)
	return nil
}

func (s *MemoryStore) SaveStageTargets(ctx context.Context, targets []StageTarget) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, target := range targets {
		key := targetKey(target.RolloutID, target.StageID)
		s.targets[key] = append(s.targets[key], target)
	}
	return nil
}

func (s *MemoryStore) ReplaceStageTargets(ctx context.Context, rolloutID string, stageID string, targets []StageTarget) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.targets[targetKey(rolloutID, stageID)] = append([]StageTarget(nil), targets...)
	return nil
}

func (s *MemoryStore) ListStageTargets(ctx context.Context, rolloutID string, stageID string) ([]StageTarget, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	targets := s.targets[targetKey(rolloutID, stageID)]
	return append([]StageTarget(nil), targets...), nil
}

func (s *MemoryStore) NextSnapshotRevision(ctx context.Context) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	revision := s.nextRevision
	s.nextRevision++
	return revision, nil
}

func rolloutActiveKey(configDefinitionID string, environment string) string {
	return configDefinitionID + "\x00" + environment
}

func targetKey(rolloutID string, stageID string) string {
	return rolloutID + "\x00" + stageID
}

func cloneRollout(rollout Rollout) Rollout {
	rollout.TargetServices = append([]string(nil), rollout.TargetServices...)
	rollout.Guardrails = append([]guardrail.Rule(nil), rollout.Guardrails...)
	if rollout.GuardrailFailures != nil {
		failures := make(map[string]int, len(rollout.GuardrailFailures))
		for name, count := range rollout.GuardrailFailures {
			failures[name] = count
		}
		rollout.GuardrailFailures = failures
	}
	return rollout
}

package agentregistry

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"config-rollout-plane/internal/domain"
)

type MemoryStore struct {
	mu sync.RWMutex

	agents           map[string]domain.Agent
	credentials      map[string]domain.AgentCredential
	acknowledgements map[string]domain.AgentAcknowledgement
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		agents:           make(map[string]domain.Agent),
		credentials:      make(map[string]domain.AgentCredential),
		acknowledgements: make(map[string]domain.AgentAcknowledgement),
	}
}

func (s *MemoryStore) SaveAgent(ctx context.Context, agent domain.Agent) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	agent.Labels = cloneLabels(agent.Labels)
	s.agents[agent.ID] = agent
	return nil
}

func (s *MemoryStore) GetAgent(ctx context.Context, agentID string) (domain.Agent, error) {
	if err := ctx.Err(); err != nil {
		return domain.Agent{}, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	agent, ok := s.agents[agentID]
	if !ok {
		return domain.Agent{}, fmt.Errorf("%w: agent %q", ErrNotFound, agentID)
	}
	agent.Labels = cloneLabels(agent.Labels)
	return agent, nil
}

func (s *MemoryStore) ListAgents(ctx context.Context) ([]domain.Agent, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	agents := make([]domain.Agent, 0, len(s.agents))
	for _, agent := range s.agents {
		agent.Labels = cloneLabels(agent.Labels)
		agents = append(agents, agent)
	}
	slices.SortFunc(agents, func(a, b domain.Agent) int {
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		return 0
	})
	return agents, nil
}

func (s *MemoryStore) SaveCredential(ctx context.Context, credential domain.AgentCredential) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.credentials[credential.Token] = credential
	return nil
}

func (s *MemoryStore) GetCredential(ctx context.Context, token string) (domain.AgentCredential, error) {
	if err := ctx.Err(); err != nil {
		return domain.AgentCredential{}, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	credential, ok := s.credentials[token]
	if !ok {
		return domain.AgentCredential{}, fmt.Errorf("%w: credential", ErrNotFound)
	}
	return credential, nil
}

func (s *MemoryStore) TouchHeartbeat(ctx context.Context, agentID string, seenAt time.Time) (domain.Agent, error) {
	if err := ctx.Err(); err != nil {
		return domain.Agent{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	agent, ok := s.agents[agentID]
	if !ok {
		return domain.Agent{}, fmt.Errorf("%w: agent %q", ErrNotFound, agentID)
	}
	agent.LastSeenAt = seenAt
	s.agents[agentID] = agent
	agent.Labels = cloneLabels(agent.Labels)
	return agent, nil
}

func (s *MemoryStore) SaveAcknowledgement(ctx context.Context, acknowledgement domain.AgentAcknowledgement) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.acknowledgements[acknowledgement.ID] = acknowledgement
	return nil
}

var _ Store = (*MemoryStore)(nil)

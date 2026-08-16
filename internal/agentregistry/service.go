package agentregistry

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"config-rollout-plane/internal/domain"
)

type Store interface {
	SaveAgent(ctx context.Context, agent domain.Agent) error
	GetAgent(ctx context.Context, agentID string) (domain.Agent, error)
	ListAgents(ctx context.Context) ([]domain.Agent, error)
	SaveCredential(ctx context.Context, credential domain.AgentCredential) error
	GetCredential(ctx context.Context, token string) (domain.AgentCredential, error)
	TouchHeartbeat(ctx context.Context, agentID string, seenAt time.Time) (domain.Agent, error)
	SaveAcknowledgement(ctx context.Context, acknowledgement domain.AgentAcknowledgement) error
}

type Service struct {
	store          Store
	bootstrapToken string
	credentialTTL  time.Duration
	now            func() time.Time
	newID          func(prefix string) string
	newToken       func() string
}

func NewService(store Store, bootstrapToken string, credentialTTL time.Duration) *Service {
	if credentialTTL == 0 {
		credentialTTL = 15 * time.Minute
	}
	return &Service{
		store:          store,
		bootstrapToken: bootstrapToken,
		credentialTTL:  credentialTTL,
		now:            func() time.Time { return time.Now().UTC() },
		newID:          randomID,
		newToken:       func() string { return randomID("cred") },
	}
}

type RegisterInput struct {
	BootstrapToken string
	ID             string
	Service        string
	Environment    domain.Environment
	Zone           string
	Instance       string
	Labels         map[string]string
}

type RegisterResult struct {
	Agent      domain.Agent
	Credential domain.AgentCredential
}

func (s *Service) Register(ctx context.Context, input RegisterInput) (RegisterResult, error) {
	if s.bootstrapToken != "" && input.BootstrapToken != s.bootstrapToken {
		return RegisterResult{}, ErrUnauthorized
	}
	if strings.TrimSpace(input.Service) == "" {
		return RegisterResult{}, fmt.Errorf("%w: service is required", ErrInvalidInput)
	}
	if strings.TrimSpace(input.Instance) == "" {
		return RegisterResult{}, fmt.Errorf("%w: instance is required", ErrInvalidInput)
	}
	if err := input.Environment.Validate(); err != nil {
		return RegisterResult{}, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}

	now := s.now()
	agentID := input.ID
	if agentID == "" {
		agentID = s.newID("agent")
	}

	agent := domain.Agent{
		ID:           agentID,
		Service:      input.Service,
		Environment:  input.Environment,
		Zone:         input.Zone,
		Instance:     input.Instance,
		Labels:       cloneLabels(input.Labels),
		RegisteredAt: now,
		LastSeenAt:   now,
	}

	credential := domain.AgentCredential{
		Token:     s.newToken(),
		AgentID:   agent.ID,
		ExpiresAt: now.Add(s.credentialTTL),
		CreatedAt: now,
	}

	if err := s.store.SaveAgent(ctx, agent); err != nil {
		return RegisterResult{}, err
	}
	if err := s.store.SaveCredential(ctx, credential); err != nil {
		return RegisterResult{}, err
	}

	return RegisterResult{Agent: agent, Credential: credential}, nil
}

func (s *Service) Verify(ctx context.Context, token string) (string, error) {
	if strings.TrimSpace(token) == "" {
		return "", ErrUnauthorized
	}

	credential, err := s.store.GetCredential(ctx, token)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return "", ErrUnauthorized
		}
		return "", err
	}

	if !credential.ExpiresAt.After(s.now()) {
		return "", ErrExpiredCredential
	}
	return credential.AgentID, nil
}

func (s *Service) ListAgents(ctx context.Context) ([]domain.Agent, error) {
	return s.store.ListAgents(ctx)
}

func (s *Service) GetAgent(ctx context.Context, agentID string) (domain.Agent, error) {
	return s.store.GetAgent(ctx, agentID)
}

func (s *Service) Heartbeat(ctx context.Context, agentID string, token string) (domain.Agent, error) {
	subject, err := s.Verify(ctx, token)
	if err != nil {
		return domain.Agent{}, err
	}
	if subject != agentID {
		return domain.Agent{}, ErrForbidden
	}
	return s.store.TouchHeartbeat(ctx, agentID, s.now())
}

type AcknowledgeInput struct {
	AgentID            string
	Token              string
	ConfigDefinitionID string
	VersionID          string
	SnapshotRevision   int64
}

func (s *Service) Acknowledge(ctx context.Context, input AcknowledgeInput) (domain.AgentAcknowledgement, error) {
	subject, err := s.Verify(ctx, input.Token)
	if err != nil {
		return domain.AgentAcknowledgement{}, err
	}
	if subject != input.AgentID {
		return domain.AgentAcknowledgement{}, ErrForbidden
	}
	if input.ConfigDefinitionID == "" || input.VersionID == "" || input.SnapshotRevision < 1 {
		return domain.AgentAcknowledgement{}, fmt.Errorf("%w: acknowledgement is incomplete", ErrInvalidInput)
	}

	ack := domain.AgentAcknowledgement{
		ID:                 s.newID("ack"),
		AgentID:            input.AgentID,
		ConfigDefinitionID: input.ConfigDefinitionID,
		VersionID:          input.VersionID,
		SnapshotRevision:   input.SnapshotRevision,
		Counted:            true,
		CreatedAt:          s.now(),
	}
	if err := s.store.SaveAcknowledgement(ctx, ack); err != nil {
		return domain.AgentAcknowledgement{}, err
	}
	return ack, nil
}

func cloneLabels(labels map[string]string) map[string]string {
	if len(labels) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(labels))
	for key, value := range labels {
		cloned[key] = value
	}
	return cloned
}

func randomID(prefix string) string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return prefix + "_" + hex.EncodeToString(b[:])
}

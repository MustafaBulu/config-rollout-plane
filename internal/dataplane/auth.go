package dataplane

import (
	"context"
	"errors"

	"config-rollout-plane/internal/agentregistry"
)

var (
	ErrMissingCredential = errors.New("missing credential")
	ErrInvalidCredential = errors.New("invalid credential")
	ErrForbidden         = errors.New("credential subject does not match agent")
	ErrSnapshotNotFound  = errors.New("snapshot not found")
)

type CredentialVerifier interface {
	Verify(ctx context.Context, token string) (agentID string, err error)
}

type AgentCredentialVerifier struct {
	agents interface {
		Verify(ctx context.Context, token string) (string, error)
	}
}

func NewAgentCredentialVerifier(agents interface {
	Verify(ctx context.Context, token string) (string, error)
}) AgentCredentialVerifier {
	return AgentCredentialVerifier{agents: agents}
}

func (v AgentCredentialVerifier) Verify(ctx context.Context, token string) (string, error) {
	agentID, err := v.agents.Verify(ctx, token)
	if err == nil {
		return agentID, nil
	}
	if errors.Is(err, agentregistry.ErrUnauthorized) {
		return "", ErrInvalidCredential
	}
	if errors.Is(err, agentregistry.ErrExpiredCredential) {
		return "", ErrInvalidCredential
	}
	return "", err
}

type StaticCredentialVerifier struct {
	tokens map[string]string
}

func NewStaticCredentialVerifier(tokens map[string]string) StaticCredentialVerifier {
	copied := make(map[string]string, len(tokens))
	for token, agentID := range tokens {
		copied[token] = agentID
	}
	return StaticCredentialVerifier{tokens: copied}
}

func (v StaticCredentialVerifier) Verify(ctx context.Context, token string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	if token == "" {
		return "", ErrMissingCredential
	}

	agentID, ok := v.tokens[token]
	if !ok {
		return "", ErrInvalidCredential
	}
	return agentID, nil
}

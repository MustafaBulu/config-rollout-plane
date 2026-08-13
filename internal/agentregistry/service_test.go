package agentregistry

import (
	"context"
	"errors"
	"testing"
	"time"

	"config-rollout-plane/internal/domain"
)

func TestServiceRegisterHeartbeatAndAcknowledge(t *testing.T) {
	ctx := context.Background()
	service := NewService(NewMemoryStore(), "bootstrap-secret", time.Hour)

	result, err := service.Register(ctx, RegisterInput{
		BootstrapToken: "bootstrap-secret",
		ID:             "agent-1",
		Service:        "payment-api",
		Environment:    domain.EnvironmentProduction,
		Instance:       "payment-api-1",
		Labels:         map[string]string{"zone": "eu-west-1a"},
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if result.Credential.Token == "" {
		t.Fatal("expected credential token")
	}

	agent, err := service.Heartbeat(ctx, "agent-1", result.Credential.Token)
	if err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	if agent.ID != "agent-1" {
		t.Fatalf("expected agent-1, got %q", agent.ID)
	}

	ack, err := service.Acknowledge(ctx, AcknowledgeInput{
		AgentID:            "agent-1",
		Token:              result.Credential.Token,
		ConfigDefinitionID: "cfg-1",
		VersionID:          "ver-1",
		SnapshotRevision:   1,
	})
	if err != nil {
		t.Fatalf("acknowledge: %v", err)
	}
	if !ack.Counted {
		t.Fatal("expected acknowledgement to be counted")
	}
}

func TestServiceRejectsWrongBootstrapToken(t *testing.T) {
	service := NewService(NewMemoryStore(), "bootstrap-secret", time.Hour)

	_, err := service.Register(context.Background(), RegisterInput{
		BootstrapToken: "wrong",
		Service:        "payment-api",
		Environment:    domain.EnvironmentProduction,
		Instance:       "payment-api-1",
	})
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized, got %v", err)
	}
}

func TestServiceRejectsCredentialSubjectMismatch(t *testing.T) {
	ctx := context.Background()
	service := NewService(NewMemoryStore(), "bootstrap-secret", time.Hour)

	result, err := service.Register(ctx, RegisterInput{
		BootstrapToken: "bootstrap-secret",
		ID:             "agent-1",
		Service:        "payment-api",
		Environment:    domain.EnvironmentProduction,
		Instance:       "payment-api-1",
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	_, err = service.Heartbeat(ctx, "agent-2", result.Credential.Token)
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected ErrForbidden, got %v", err)
	}
}

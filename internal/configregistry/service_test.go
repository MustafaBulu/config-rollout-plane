package configregistry

import (
	"context"
	"errors"
	"testing"

	"config-rollout-plane/internal/domain"
)

func TestServiceConfigRegistryAcceptanceFlow(t *testing.T) {
	ctx := context.Background()
	service := NewService(NewMemoryStore(), JSONSchemaValidator{})

	tenant, err := service.CreateTenant(ctx, CreateTenantInput{
		ID:   "payments",
		Name: "Payments",
	})
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	if tenant.ID != "payments" {
		t.Fatalf("expected tenant payments, got %q", tenant.ID)
	}

	definition, err := service.CreateDefinition(ctx, CreateDefinitionInput{
		TenantID:     "payments",
		Key:          "payment.authorization.timeout",
		Description:  "Authorization provider timeout",
		Schema:       []byte(`{"type":"integer","minimum":100,"maximum":10000}`),
		DefaultValue: []byte(`2000`),
	})
	if err != nil {
		t.Fatalf("create definition: %v", err)
	}

	version, err := service.CreateVersion(ctx, CreateVersionInput{
		TenantID:  "payments",
		Key:       "payment.authorization.timeout",
		Value:     []byte(`1500`),
		CreatedBy: "developer@example.com",
	})
	if err != nil {
		t.Fatalf("create version: %v", err)
	}
	if version.VersionNumber != 1 {
		t.Fatalf("expected version number 1, got %d", version.VersionNumber)
	}

	state, err := service.SetStableVersion(ctx, SetStableVersionInput{
		TenantID:      "payments",
		Key:           "payment.authorization.timeout",
		Environment:   domain.EnvironmentProduction,
		VersionNumber: 1,
	})
	if err != nil {
		t.Fatalf("set stable version: %v", err)
	}
	if state.StableVersionID != version.ID {
		t.Fatalf("expected stable version %q, got %q", version.ID, state.StableVersionID)
	}

	gotDefinition, err := service.GetDefinition(ctx, "payments", "payment.authorization.timeout")
	if err != nil {
		t.Fatalf("get definition: %v", err)
	}
	if gotDefinition.ID != definition.ID {
		t.Fatalf("expected definition %q, got %q", definition.ID, gotDefinition.ID)
	}
}

func TestServiceRejectsDefaultValueOutsideSchema(t *testing.T) {
	ctx := context.Background()
	service := NewService(NewMemoryStore(), JSONSchemaValidator{})

	if _, err := service.CreateTenant(ctx, CreateTenantInput{ID: "payments", Name: "Payments"}); err != nil {
		t.Fatalf("create tenant: %v", err)
	}

	_, err := service.CreateDefinition(ctx, CreateDefinitionInput{
		TenantID:     "payments",
		Key:          "payment.authorization.timeout",
		Schema:       []byte(`{"type":"integer","minimum":100,"maximum":10000}`),
		DefaultValue: []byte(`50`),
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput, got %v", err)
	}
}

func TestServiceRejectsVersionValueOutsideSchema(t *testing.T) {
	ctx := context.Background()
	service := NewService(NewMemoryStore(), JSONSchemaValidator{})

	if _, err := service.CreateTenant(ctx, CreateTenantInput{ID: "payments", Name: "Payments"}); err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	if _, err := service.CreateDefinition(ctx, CreateDefinitionInput{
		TenantID:     "payments",
		Key:          "payment.authorization.timeout",
		Schema:       []byte(`{"type":"integer","minimum":100,"maximum":10000}`),
		DefaultValue: []byte(`2000`),
	}); err != nil {
		t.Fatalf("create definition: %v", err)
	}

	_, err := service.CreateVersion(ctx, CreateVersionInput{
		TenantID: "payments",
		Key:      "payment.authorization.timeout",
		Value:    []byte(`20000`),
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput, got %v", err)
	}
}

func TestServiceRejectsDuplicateConfigKeyForTenant(t *testing.T) {
	ctx := context.Background()
	service := NewService(NewMemoryStore(), JSONSchemaValidator{})

	if _, err := service.CreateTenant(ctx, CreateTenantInput{ID: "payments", Name: "Payments"}); err != nil {
		t.Fatalf("create tenant: %v", err)
	}

	input := CreateDefinitionInput{
		TenantID: "payments",
		Key:      "payment.authorization.timeout",
		Schema:   []byte(`{"type":"integer"}`),
	}
	if _, err := service.CreateDefinition(ctx, input); err != nil {
		t.Fatalf("create first definition: %v", err)
	}

	_, err := service.CreateDefinition(ctx, input)
	if !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("expected ErrAlreadyExists, got %v", err)
	}
}

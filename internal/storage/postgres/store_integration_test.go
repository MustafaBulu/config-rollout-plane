package postgres

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"config-rollout-plane/internal/configregistry"
	"config-rollout-plane/internal/domain"
)

func TestStoreConfigRegistryAcceptanceFlow(t *testing.T) {
	databaseURL := os.Getenv("SAFE_CONFIG_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("set SAFE_CONFIG_TEST_DATABASE_URL to run postgres integration tests")
	}

	ctx := context.Background()
	store, err := NewStore(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect postgres: %v", err)
	}
	defer store.Close()

	applyMigrations(t, ctx, store)

	service := configregistry.NewService(store, configregistry.JSONSchemaValidator{})
	tenantID := fmt.Sprintf("payments_pg_test_%d", time.Now().UnixNano())

	_, err = service.CreateTenant(ctx, configregistry.CreateTenantInput{
		ID:   tenantID,
		Name: "Payments",
	})
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}

	definition, err := service.CreateDefinition(ctx, configregistry.CreateDefinitionInput{
		TenantID:     tenantID,
		Key:          "payment.authorization.timeout",
		Schema:       []byte(`{"type":"integer","minimum":100,"maximum":10000}`),
		DefaultValue: []byte(`2000`),
	})
	if err != nil {
		t.Fatalf("create definition: %v", err)
	}

	version, err := service.CreateVersion(ctx, configregistry.CreateVersionInput{
		TenantID:  tenantID,
		Key:       "payment.authorization.timeout",
		Value:     []byte(`1500`),
		CreatedBy: "developer@example.com",
	})
	if err != nil {
		t.Fatalf("create version: %v", err)
	}

	state, err := service.SetStableVersion(ctx, configregistry.SetStableVersionInput{
		TenantID:      tenantID,
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

	got, err := service.GetDefinition(ctx, tenantID, "payment.authorization.timeout")
	if err != nil {
		t.Fatalf("get definition: %v", err)
	}
	if got.ID != definition.ID {
		t.Fatalf("expected definition %q, got %q", definition.ID, got.ID)
	}
}

func applyMigrations(t *testing.T, ctx context.Context, store *Store) {
	t.Helper()

	for _, name := range []string{"000001_foundation.sql", "000002_config_registry.sql", "000003_agents.sql", "000004_rollouts.sql", "000005_guardrails.sql"} {
		path := filepath.Join("..", "..", "..", "migrations", name)
		sql, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read migration %s: %v", name, err)
		}
		if _, err := store.pool.Exec(ctx, string(sql)); err != nil {
			t.Fatalf("apply migration %s: %v", name, err)
		}
	}
}

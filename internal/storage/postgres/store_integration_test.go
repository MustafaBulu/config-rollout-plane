package postgres

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"config-rollout-plane/internal/agentregistry"
	"config-rollout-plane/internal/configregistry"
	"config-rollout-plane/internal/domain"
	"config-rollout-plane/internal/rollout"
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

func TestStoreAllowsOnlyOneConcurrentActiveRollout(t *testing.T) {
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

	registry := configregistry.NewService(store, configregistry.JSONSchemaValidator{})
	agents := agentregistry.NewService(store, "bootstrap", time.Hour)
	tenantID := fmt.Sprintf("payments_pg_race_%d", time.Now().UnixNano())
	key := "payment.failure_rate"

	_, err = registry.CreateTenant(ctx, configregistry.CreateTenantInput{
		ID:   tenantID,
		Name: "Payments",
	})
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}

	definition, err := registry.CreateDefinition(ctx, configregistry.CreateDefinitionInput{
		TenantID:     tenantID,
		Key:          key,
		Schema:       []byte(`{"type":"number","minimum":0,"maximum":1}`),
		DefaultValue: []byte(`0`),
	})
	if err != nil {
		t.Fatalf("create definition: %v", err)
	}

	stable, err := registry.CreateVersion(ctx, configregistry.CreateVersionInput{
		TenantID:  tenantID,
		Key:       key,
		Value:     []byte(`0`),
		CreatedBy: "test",
	})
	if err != nil {
		t.Fatalf("create stable version: %v", err)
	}
	if _, err := registry.CreateVersion(ctx, configregistry.CreateVersionInput{
		TenantID:  tenantID,
		Key:       key,
		Value:     []byte(`0.2`),
		CreatedBy: "test",
	}); err != nil {
		t.Fatalf("create candidate version: %v", err)
	}
	state, err := registry.SetStableVersion(ctx, configregistry.SetStableVersionInput{
		TenantID:      tenantID,
		Key:           key,
		Environment:   domain.EnvironmentProduction,
		VersionNumber: 1,
	})
	if err != nil {
		t.Fatalf("set stable version: %v", err)
	}
	if state.StableVersionID != stable.ID {
		t.Fatalf("expected stable version %q, got %q", stable.ID, state.StableVersionID)
	}

	const attempts = 8
	start := make(chan struct{})
	errs := make(chan error, attempts)
	var wg sync.WaitGroup

	for i := range attempts {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			<-start
			service := rollout.NewService(store, registry, agents)
			_, _, err := service.CreateRollout(ctx, rollout.CreateRolloutInput{
				TenantID:               tenantID,
				Key:                    key,
				Environment:            domain.EnvironmentProduction,
				CandidateVersionNumber: 2,
				TargetServices:         []string{"payment-service"},
				Stages: []rollout.Stage{
					{ID: fmt.Sprintf("stage-100-%d", index), Percentage: 100},
				},
				RequiredAckPercentage: 100,
				DeploymentTimeout:     time.Minute,
			})
			errs <- err
		}(i)
	}

	close(start)
	wg.Wait()
	close(errs)

	successes := 0
	conflicts := 0
	for err := range errs {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, rollout.ErrConflict):
			conflicts++
		default:
			t.Fatalf("unexpected rollout create error: %v", err)
		}
	}

	if successes != 1 {
		t.Fatalf("expected exactly one successful active rollout create, got successes=%d conflicts=%d", successes, conflicts)
	}

	active, err := store.GetActiveRollout(ctx, definition.ID, string(domain.EnvironmentProduction))
	if err != nil {
		t.Fatalf("get active rollout: %v", err)
	}
	if active.ConfigDefinitionID != definition.ID || active.Environment != domain.EnvironmentProduction {
		t.Fatalf("active rollout points at wrong config/environment: %+v", active)
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

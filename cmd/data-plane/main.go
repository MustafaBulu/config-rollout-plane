package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"config-rollout-plane/internal/agentregistry"
	"config-rollout-plane/internal/configregistry"
	"config-rollout-plane/internal/dataplane"
	"config-rollout-plane/internal/logging"
	"config-rollout-plane/internal/rollout"
	"config-rollout-plane/internal/runtime"
	postgresstore "config-rollout-plane/internal/storage/postgres"
)

func main() {
	const serviceName = "data-plane"

	logger := logging.New(serviceName)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg := runtime.HTTPServerConfig{
		ServiceName:     serviceName,
		Addr:            runtime.EnvString("DATA_PLANE_ADDR", ":8081"),
		ReadTimeout:     runtime.EnvDuration("DATA_PLANE_READ_TIMEOUT", 5*time.Second),
		WriteTimeout:    runtime.EnvDuration("DATA_PLANE_WRITE_TIMEOUT", 10*time.Second),
		IdleTimeout:     runtime.EnvDuration("DATA_PLANE_IDLE_TIMEOUT", 60*time.Second),
		ShutdownTimeout: runtime.EnvDuration("DATA_PLANE_SHUTDOWN_TIMEOUT", 10*time.Second),
	}

	snapshotStore, verifier, cleanup := openDataPlaneDependencies(ctx, logger)
	defer cleanup()

	handler := dataplane.NewHandler(snapshotStore, verifier)
	if err := runtime.RunHTTPServer(ctx, cfg, handler, logger); err != nil {
		logger.Error("service stopped with error", slog.Any("error", err))
		os.Exit(1)
	}
}

func openDataPlaneDependencies(ctx context.Context, logger *slog.Logger) (dataplane.SnapshotStore, dataplane.CredentialVerifier, func()) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		snapshotStore := dataplane.NewMemorySnapshotStore()
		seedSnapshot(snapshotStore, logger)
		return snapshotStore, dataplane.NewStaticCredentialVerifier(agentTokens()), func() {}
	}

	connectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	store, err := postgresstore.NewStore(connectCtx, databaseURL)
	if err != nil {
		logger.Error("postgres connection failed", slog.Any("error", err))
		os.Exit(1)
	}

	registry := configregistry.NewService(store, configregistry.JSONSchemaValidator{})
	agents := agentregistry.NewService(store, "", runtime.EnvDuration("AGENT_CREDENTIAL_TTL", 15*time.Minute))
	rollouts := rollout.NewService(store, registry, agents)
	snapshotStore := dataplane.NewDynamicSnapshotStore(registry, agents, rollouts, tenantFilter())

	logger.Info("using postgres dynamic snapshot store")
	return snapshotStore, dataplane.NewAgentCredentialVerifier(agents), store.Close
}

func agentTokens() map[string]string {
	agentID := os.Getenv("DATA_PLANE_AGENT_ID")
	token := os.Getenv("DATA_PLANE_AGENT_TOKEN")
	if agentID == "" || token == "" {
		return nil
	}
	return map[string]string{token: agentID}
}

func tenantFilter() []string {
	raw := os.Getenv("DATA_PLANE_TENANTS")
	if raw == "" {
		raw = os.Getenv("DATA_PLANE_TENANT_ID")
	}
	if raw == "" {
		return nil
	}

	parts := strings.Split(raw, ",")
	tenants := make([]string, 0, len(parts))
	for _, part := range parts {
		tenant := strings.TrimSpace(part)
		if tenant != "" {
			tenants = append(tenants, tenant)
		}
	}
	return tenants
}

func seedSnapshot(store *dataplane.MemorySnapshotStore, logger *slog.Logger) {
	agentID := os.Getenv("DATA_PLANE_AGENT_ID")
	key := os.Getenv("DATA_PLANE_CONFIG_KEY")
	value := os.Getenv("DATA_PLANE_CONFIG_VALUE")
	if agentID == "" || key == "" || value == "" {
		return
	}

	raw := json.RawMessage(value)
	if !json.Valid(raw) {
		logger.Error("DATA_PLANE_CONFIG_VALUE must be valid JSON")
		return
	}

	store.PutSnapshot(agentID, dataplane.Snapshot{
		Revision:    1,
		GeneratedAt: time.Now().UTC(),
		Configs: []dataplane.SnapshotItem{
			{
				ConfigDefinitionID: runtime.EnvString("DATA_PLANE_CONFIG_DEFINITION_ID", "cfg-dev"),
				Key:                key,
				VersionID:          runtime.EnvString("DATA_PLANE_CONFIG_VERSION_ID", "ver-dev"),
				Version:            1,
				Value:              raw,
				Checksum:           dataplane.Checksum(raw),
			},
		},
	})
}

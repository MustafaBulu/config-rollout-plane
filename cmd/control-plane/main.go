package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"config-rollout-plane/internal/agentregistry"
	"config-rollout-plane/internal/configregistry"
	"config-rollout-plane/internal/controlplane"
	"config-rollout-plane/internal/health"
	"config-rollout-plane/internal/logging"
	"config-rollout-plane/internal/runtime"
	postgresstore "config-rollout-plane/internal/storage/postgres"
)

func main() {
	const serviceName = "control-plane"

	logger := logging.New(serviceName)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg := runtime.HTTPServerConfig{
		ServiceName:     serviceName,
		Addr:            runtime.EnvString("CONTROL_PLANE_ADDR", ":8080"),
		ReadTimeout:     runtime.EnvDuration("CONTROL_PLANE_READ_TIMEOUT", 5*time.Second),
		WriteTimeout:    runtime.EnvDuration("CONTROL_PLANE_WRITE_TIMEOUT", 10*time.Second),
		IdleTimeout:     runtime.EnvDuration("CONTROL_PLANE_IDLE_TIMEOUT", 60*time.Second),
		ShutdownTimeout: runtime.EnvDuration("CONTROL_PLANE_SHUTDOWN_TIMEOUT", 10*time.Second),
	}

	store, readiness, cleanup := openStore(ctx, logger)
	defer cleanup()
	registry := configregistry.NewService(store, configregistry.JSONSchemaValidator{})
	agents := agentregistry.NewService(
		agentregistry.NewMemoryStore(),
		runtime.EnvString("AGENT_BOOTSTRAP_TOKEN", "dev-bootstrap-token"),
		runtime.EnvDuration("AGENT_CREDENTIAL_TTL", 15*time.Minute),
	)
	handler := controlplane.NewHandlerWithReadiness(registry, agents, readiness)
	if err := runtime.RunHTTPServer(ctx, cfg, handler, logger); err != nil {
		logger.Error("service stopped with error", slog.Any("error", err))
		os.Exit(1)
	}
}

func openStore(ctx context.Context, logger *slog.Logger) (configregistry.Store, health.Checker, func()) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		logger.Warn("DATABASE_URL is not set; using in-memory config registry store")
		store := configregistry.NewMemoryStore()
		return store, health.StaticChecker{}, func() {}
	}

	connectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	store, err := postgresstore.NewStore(connectCtx, databaseURL)
	if err != nil {
		logger.Error("postgres connection failed", slog.Any("error", err))
		os.Exit(1)
	}

	logger.Info("using postgres config registry store")
	return store, store, store.Close
}

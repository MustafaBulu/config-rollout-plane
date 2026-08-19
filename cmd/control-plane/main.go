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
	"config-rollout-plane/internal/guardrail"
	"config-rollout-plane/internal/health"
	"config-rollout-plane/internal/logging"
	"config-rollout-plane/internal/rollout"
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

	stores, cleanup := openStores(ctx, logger)
	defer cleanup()
	registry := configregistry.NewService(stores.registry, configregistry.JSONSchemaValidator{})
	agents := agentregistry.NewService(
		stores.agents,
		runtime.EnvString("AGENT_BOOTSTRAP_TOKEN", "dev-bootstrap-token"),
		runtime.EnvDuration("AGENT_CREDENTIAL_TTL", 15*time.Minute),
	)
	rollouts := rollout.NewService(stores.rollouts, registry, agents)
	configureGuardrails(rollouts, logger)
	startRolloutReconciler(ctx, rollouts, logger)
	handler := controlplane.NewHandlerWithServices(registry, agents, rollouts, stores.readiness)
	if err := runtime.RunHTTPServer(ctx, cfg, handler, logger); err != nil {
		logger.Error("service stopped with error", slog.Any("error", err))
		os.Exit(1)
	}
}

func configureGuardrails(rollouts *rollout.Service, logger *slog.Logger) {
	prometheusURL := os.Getenv("PROMETHEUS_URL")
	if prometheusURL == "" {
		return
	}

	client, err := guardrail.NewPrometheusClient(
		prometheusURL,
		runtime.EnvDuration("PROMETHEUS_QUERY_TIMEOUT", 2*time.Second),
	)
	if err != nil {
		logger.Error("prometheus guardrail client disabled", slog.Any("error", err))
		return
	}
	rollouts.SetGuardrailQueryer(client)
	logger.Info("prometheus guardrail client enabled", slog.String("url", prometheusURL))
}

type stores struct {
	registry  configregistry.Store
	agents    agentregistry.Store
	rollouts  rollout.Store
	readiness health.Checker
}

func openStores(ctx context.Context, logger *slog.Logger) (stores, func()) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		logger.Warn("DATABASE_URL is not set; using in-memory stores")
		return stores{
			registry:  configregistry.NewMemoryStore(),
			agents:    agentregistry.NewMemoryStore(),
			rollouts:  rollout.NewMemoryStore(),
			readiness: health.StaticChecker{},
		}, func() {}
	}

	connectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	store, err := postgresstore.NewStore(connectCtx, databaseURL)
	if err != nil {
		logger.Error("postgres connection failed", slog.Any("error", err))
		os.Exit(1)
	}

	logger.Info("using postgres stores")
	return stores{
		registry:  store,
		agents:    store,
		rollouts:  store,
		readiness: store,
	}, store.Close
}

func startRolloutReconciler(ctx context.Context, rollouts *rollout.Service, logger *slog.Logger) {
	interval := runtime.EnvDuration("ROLLOUT_RECONCILE_INTERVAL", 2*time.Second)
	if interval <= 0 {
		return
	}

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := rollouts.ReconcileActive(ctx); err != nil {
					logger.Error("rollout reconcile failed", slog.Any("error", err))
				}
			}
		}
	}()
}

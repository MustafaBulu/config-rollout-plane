package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"config-rollout-plane/internal/agent"
	"config-rollout-plane/internal/logging"
	"config-rollout-plane/internal/runtime"
)

func main() {
	const serviceName = "agent"

	logger := logging.New(serviceName)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg := runtime.HTTPServerConfig{
		ServiceName:     serviceName,
		Addr:            runtime.EnvString("AGENT_ADDR", ":8082"),
		ReadTimeout:     runtime.EnvDuration("AGENT_READ_TIMEOUT", 5*time.Second),
		WriteTimeout:    runtime.EnvDuration("AGENT_WRITE_TIMEOUT", 10*time.Second),
		IdleTimeout:     runtime.EnvDuration("AGENT_IDLE_TIMEOUT", 60*time.Second),
		ShutdownTimeout: runtime.EnvDuration("AGENT_SHUTDOWN_TIMEOUT", 10*time.Second),
	}

	cache := agent.NewSnapshotCache(runtime.EnvString("AGENT_CACHE_PATH", "var/safeconfig/snapshot.json"))
	if err := cache.Load(ctx); err != nil {
		logger.Warn("snapshot cache not loaded", slog.Any("error", err))
	}

	dataPlaneURL := os.Getenv("DATA_PLANE_URL")
	agentID := runtime.EnvString("AGENT_ID", hostname())
	instanceCredential := os.Getenv("AGENT_INSTANCE_CREDENTIAL")
	controlPlaneURL := os.Getenv("CONTROL_PLANE_URL")
	if instanceCredential == "" && controlPlaneURL != "" {
		result, err := registerAgent(ctx, controlPlaneURL, agentID)
		if err != nil {
			logger.Warn("agent registration failed", slog.Any("error", err))
		} else {
			agentID = result.AgentID
			instanceCredential = result.InstanceCredential
			logger.Info("agent registered", slog.String("agent_id", agentID))
		}
	}
	if dataPlaneURL != "" && agentID != "" && instanceCredential != "" {
		var acknowledger agent.Acknowledger
		if controlPlaneURL != "" {
			acknowledger = agent.ControlPlaneAcknowledger{
				BaseURL: controlPlaneURL,
			}
		}
		syncer := &agent.Syncer{
			Client: agent.SnapshotClient{
				BaseURL: dataPlaneURL,
				AgentID: agentID,
				Token:   instanceCredential,
			},
			Acknowledger: acknowledger,
			Cache:        cache,
			PollInterval: runtime.EnvDuration("AGENT_POLL_INTERVAL", 2*time.Second),
		}
		go func() {
			if err := syncer.Run(ctx); err != nil && ctx.Err() == nil {
				logger.Error("agent sync stopped", slog.Any("error", err))
			}
		}()
	} else {
		logger.Warn("agent remote sync disabled; DATA_PLANE_URL, AGENT_ID, or AGENT_INSTANCE_CREDENTIAL is missing")
	}

	handler := agent.NewHandler(cache)
	if err := runtime.RunHTTPServer(ctx, cfg, handler, logger); err != nil {
		logger.Error("service stopped with error", slog.Any("error", err))
		os.Exit(1)
	}
}

func registerAgent(ctx context.Context, controlPlaneURL string, agentID string) (agent.RegisterResult, error) {
	return agent.RegistrationClient{BaseURL: controlPlaneURL}.Register(ctx, agent.RegisterInput{
		BootstrapToken: os.Getenv("AGENT_BOOTSTRAP_TOKEN"),
		ID:             agentID,
		Service:        runtime.EnvString("AGENT_SERVICE", runtime.EnvString("SERVICE_NAME", "payment-service")),
		Environment:    runtime.EnvString("AGENT_ENVIRONMENT", runtime.EnvString("ENVIRONMENT", "production")),
		Zone:           os.Getenv("AGENT_ZONE"),
		Instance:       runtime.EnvString("AGENT_INSTANCE", agentID),
	})
}

func hostname() string {
	name, err := os.Hostname()
	if err != nil || name == "" {
		return "agent-local"
	}
	return name
}

package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"config-rollout-plane/internal/health"
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

	handler := health.NewHandler(serviceName, health.StaticChecker{})
	if err := runtime.RunHTTPServer(ctx, cfg, handler, logger); err != nil {
		logger.Error("service stopped with error", slog.Any("error", err))
		os.Exit(1)
	}
}

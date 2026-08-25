package reliability

import (
	"context"
	"errors"
	"fmt"

	"config-rollout-plane/internal/agent"
)

const paymentFailureRateKey = "payment.failure_rate"

func RunControlPlaneRestartScenario(ctx context.Context, options Options) (ScenarioResult, error) {
	harness, err := NewHarness(ctx, options)
	if err != nil {
		return ScenarioResult{}, err
	}
	defer harness.Close()

	var identity AgentIdentity
	runner := Runner{
		Name: "control-plane-restart",
		Steps: []Step{
			{
				Name: "start control plane",
				Run: func(ctx context.Context, h *Harness, result *ScenarioResult) error {
					h.StartControlPlane()
					result.addMetric("control_plane_starts", 1)
					return nil
				},
			},
			{
				Name: "register agent before restart",
				Run: func(ctx context.Context, h *Harness, result *ScenarioResult) error {
					var err error
					identity, err = h.RegisterAgent(ctx, "agent-restart-1")
					if err == nil {
						result.addMetric("registered_agents", 1)
					}
					return err
				},
			},
			{
				Name: "restart control plane",
				Run: func(ctx context.Context, h *Harness, result *ScenarioResult) error {
					if err := h.Restart(FailureControlPlane); err != nil {
						return err
					}
					result.addMetric("control_plane_restarts", 1)
					return nil
				},
			},
			{
				Name: "heartbeat through restarted control plane",
				Run: func(ctx context.Context, h *Harness, result *ScenarioResult) error {
					if err := h.Heartbeat(ctx, identity); err != nil {
						return err
					}
					result.addMetric("post_restart_heartbeats", 1)
					return nil
				},
			},
		},
	}
	return runner.Run(ctx, harness)
}

func RunDataPlaneOutageScenario(ctx context.Context, options Options) (ScenarioResult, error) {
	harness, err := NewHarness(ctx, options)
	if err != nil {
		return ScenarioResult{}, err
	}
	defer harness.Close()

	var identity AgentIdentity
	var syncer *agent.Syncer
	var cache *agent.SnapshotCache

	runner := Runner{
		Name: "data-plane-outage",
		Steps: []Step{
			{
				Name: "start control and data planes",
				Run: func(ctx context.Context, h *Harness, result *ScenarioResult) error {
					h.StartControlPlane()
					h.StartDataPlane()
					result.addMetric("control_plane_starts", 1)
					result.addMetric("data_plane_starts", 1)
					return nil
				},
			},
			{
				Name: "register agent",
				Run: func(ctx context.Context, h *Harness, result *ScenarioResult) error {
					var err error
					identity, err = h.RegisterAgent(ctx, "agent-outage-1")
					if err == nil {
						result.addMetric("registered_agents", 1)
					}
					return err
				},
			},
			{
				Name: "warm local cache",
				Run: func(ctx context.Context, h *Harness, result *ScenarioResult) error {
					var err error
					syncer, cache, err = h.NewSyncer(identity)
					if err != nil {
						return err
					}
					if err := syncer.SyncOnce(ctx); err != nil {
						return err
					}
					value, version, err := h.CachedConfigValue(cache, paymentFailureRateKey)
					if err != nil {
						return err
					}
					if value != "0" || version != 1 {
						return fmt.Errorf("unexpected cached config: value=%s version=%d", value, version)
					}
					result.addMetric("cache_warm_configs", 1)
					return nil
				},
			},
			{
				Name: "inject data-plane outage",
				Run: func(ctx context.Context, h *Harness, result *ScenarioResult) error {
					if err := h.InjectFailure(FailureDataPlane); err != nil {
						return err
					}
					result.addMetric("data_plane_outages", 1)
					return nil
				},
			},
			{
				Name: "poll fails while cache remains usable",
				Run: func(ctx context.Context, h *Harness, result *ScenarioResult) error {
					if syncer == nil || cache == nil {
						return errors.New("cache was not warmed")
					}
					if err := syncer.SyncOnce(ctx); err == nil {
						return errors.New("expected data-plane polling to fail during outage")
					}
					value, version, err := h.CachedConfigValue(cache, paymentFailureRateKey)
					if err != nil {
						return err
					}
					if value != "0" || version != 1 {
						return fmt.Errorf("unexpected cached config during outage: value=%s version=%d", value, version)
					}
					result.addMetric("cache_reads_during_outage", 1)
					return nil
				},
			},
		},
	}
	return runner.Run(ctx, harness)
}

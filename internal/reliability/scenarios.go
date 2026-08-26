package reliability

import (
	"context"
	"errors"
	"fmt"
	"time"

	"config-rollout-plane/internal/agent"
	"config-rollout-plane/internal/guardrail"
	"config-rollout-plane/internal/rollout"
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

func RunConcurrentRolloutAcknowledgementScenario(ctx context.Context, options Options) (ScenarioResult, error) {
	harness, err := NewHarness(ctx, options)
	if err != nil {
		return ScenarioResult{}, err
	}
	defer harness.Close()

	const agentCount = 200
	var identities []AgentIdentity
	var created rollout.Rollout
	var targets []rollout.StageTarget

	runner := Runner{
		Name: "concurrent-rollout-acknowledgements",
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
				Name: "register concurrent agents",
				Run: func(ctx context.Context, h *Harness, result *ScenarioResult) error {
					started := time.Now()
					var err error
					identities, err = h.RegisterAgents(ctx, agentCount, h.concurrency, "concurrent-agent")
					if err != nil {
						return err
					}
					result.addMetric("registered_agents", len(identities))
					result.addTiming("agent_registration_duration", time.Since(started))
					return nil
				},
			},
			{
				Name: "create single-stage rollout",
				Run: func(ctx context.Context, h *Harness, result *ScenarioResult) error {
					var err error
					created, targets, err = h.CreateRollout(ctx, rollout.CreateRolloutInput{
						Stages: []rollout.Stage{{ID: "stage-100", Percentage: 100}},
					})
					if err != nil {
						return err
					}
					if len(targets) != agentCount {
						return fmt.Errorf("expected %d rollout targets, got %d", agentCount, len(targets))
					}
					result.addMetric("rollout_targets", len(targets))
					return nil
				},
			},
			{
				Name: "acknowledge rollout concurrently",
				Run: func(ctx context.Context, h *Harness, result *ScenarioResult) error {
					started := time.Now()
					acked, err := h.AcknowledgeAssignedStage(ctx, identities, created.CurrentStageID, h.concurrency)
					if err != nil {
						return err
					}
					result.addMetric("concurrent_acknowledgements", acked)
					result.addTiming("concurrent_ack_duration", time.Since(started))
					if acked != len(targets) {
						return fmt.Errorf("expected %d acknowledgements, got %d", len(targets), acked)
					}
					return nil
				},
			},
			{
				Name: "verify completed rollout",
				Run: func(ctx context.Context, h *Harness, result *ScenarioResult) error {
					final, _, coverage, err := h.GetRollout(ctx, created.ID)
					if err != nil {
						return err
					}
					if final.State != rollout.StateCompleted {
						return fmt.Errorf("expected completed rollout, got %s", final.State)
					}
					if coverage.Acked != coverage.Total || coverage.Total != len(targets) {
						return fmt.Errorf("unexpected coverage: %+v", coverage)
					}
					result.addMetric("final_coverage_percent", int(coverage.Percentage))
					return nil
				},
			},
		},
	}
	return runner.Run(ctx, harness)
}

func RunRollbackPropagationTimingScenario(ctx context.Context, options Options) (ScenarioResult, error) {
	harness, err := NewHarness(ctx, options)
	if err != nil {
		return ScenarioResult{}, err
	}
	defer harness.Close()

	const agentCount = 120
	var identities []AgentIdentity
	var created rollout.Rollout
	var candidateTargets []rollout.StageTarget
	var rollbackStarted time.Time

	runner := Runner{
		Name: "rollback-propagation-timing",
		Steps: []Step{
			{
				Name: "start control and data planes",
				Run: func(ctx context.Context, h *Harness, result *ScenarioResult) error {
					h.StartControlPlane()
					h.StartDataPlane()
					h.SetGuardrailQueryer(staticQueryer{value: 0.05})
					result.addMetric("control_plane_starts", 1)
					result.addMetric("data_plane_starts", 1)
					return nil
				},
			},
			{
				Name: "register rollback agents",
				Run: func(ctx context.Context, h *Harness, result *ScenarioResult) error {
					var err error
					identities, err = h.RegisterAgents(ctx, agentCount, h.concurrency, "rollback-agent")
					if err != nil {
						return err
					}
					result.addMetric("registered_agents", len(identities))
					return nil
				},
			},
			{
				Name: "create guarded rollout",
				Run: func(ctx context.Context, h *Harness, result *ScenarioResult) error {
					var err error
					created, candidateTargets, err = h.CreateRollout(ctx, rollout.CreateRolloutInput{
						Stages: []rollout.Stage{{ID: "stage-100", Percentage: 100}},
						Guardrails: []guardrail.Rule{
							{
								Name:                "error-rate",
								Query:               `sum(rate(payment_requests_total{result="error"}[1m]))`,
								Operator:            guardrail.OperatorLessThan,
								Threshold:           0.02,
								ConsecutiveFailures: 1,
							},
						},
						RollbackTimeout: time.Minute,
					})
					if err != nil {
						return err
					}
					result.addMetric("candidate_targets", len(candidateTargets))
					return nil
				},
			},
			{
				Name: "trigger rollback from unhealthy guardrail",
				Run: func(ctx context.Context, h *Harness, result *ScenarioResult) error {
					acked, err := h.AcknowledgeAssignedStage(ctx, identities, created.CurrentStageID, h.concurrency)
					if err != nil {
						return err
					}
					current, _, _, err := h.GetRollout(ctx, created.ID)
					if err != nil {
						return err
					}
					if current.State != rollout.StateRollingBack {
						return fmt.Errorf("expected rolling back rollout, got %s", current.State)
					}
					rollbackStarted = time.Now()
					result.addMetric("candidate_acknowledgements", acked)
					return nil
				},
			},
			{
				Name: "propagate stable rollback snapshot",
				Run: func(ctx context.Context, h *Harness, result *ScenarioResult) error {
					if err := h.ReconcileActive(ctx); err != nil {
						return err
					}
					verifying, verificationTargets, _, err := h.GetRollout(ctx, created.ID)
					if err != nil {
						return err
					}
					if verifying.State != rollout.StateRollingBack {
						return fmt.Errorf("expected rollback verification, got %s", verifying.State)
					}
					if len(verificationTargets) != len(candidateTargets) {
						return fmt.Errorf("expected %d verification targets, got %d", len(candidateTargets), len(verificationTargets))
					}
					acked, err := h.AcknowledgeAssignedStage(ctx, identities, verifying.CurrentStageID, h.concurrency)
					if err != nil {
						return err
					}
					final, _, _, err := h.GetRollout(ctx, created.ID)
					if err != nil {
						return err
					}
					if final.State != rollout.StateRolledBack {
						return fmt.Errorf("expected rolled back rollout, got %s", final.State)
					}
					if final.RollbackStatus != rollout.RollbackStatusVerified {
						return fmt.Errorf("expected verified rollback, got %s", final.RollbackStatus)
					}
					result.addMetric("rollback_verification_acknowledgements", acked)
					result.addTiming("rollback_propagation_duration", time.Since(rollbackStarted))
					return nil
				},
			},
		},
	}
	return runner.Run(ctx, harness)
}

type staticQueryer struct {
	value float64
	err   error
}

func (q staticQueryer) Query(ctx context.Context, query string, at time.Time) (float64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return q.value, q.err
}

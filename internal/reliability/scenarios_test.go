package reliability

import "testing"

func TestControlPlaneRestartScenario(t *testing.T) {
	result, err := RunControlPlaneRestartScenario(t.Context(), Options{TempDir: t.TempDir()})
	if err != nil {
		t.Fatalf("run scenario: %v", err)
	}
	if !result.Passed {
		t.Fatalf("expected scenario to pass: %+v", result)
	}
	if result.Metrics["control_plane_restarts"] != 1 {
		t.Fatalf("expected one restart, got %+v", result.Metrics)
	}
}

func TestDataPlaneOutageScenario(t *testing.T) {
	result, err := RunDataPlaneOutageScenario(t.Context(), Options{TempDir: t.TempDir()})
	if err != nil {
		t.Fatalf("run scenario: %v", err)
	}
	if !result.Passed {
		t.Fatalf("expected scenario to pass: %+v", result)
	}
	if result.Metrics["cache_reads_during_outage"] != 1 {
		t.Fatalf("expected cached read during outage, got %+v", result.Metrics)
	}
}

func TestConcurrentRolloutAcknowledgementScenario(t *testing.T) {
	result, err := RunConcurrentRolloutAcknowledgementScenario(t.Context(), Options{TempDir: t.TempDir(), Concurrency: 32})
	if err != nil {
		t.Fatalf("run scenario: %v", err)
	}
	if !result.Passed {
		t.Fatalf("expected scenario to pass: %+v", result)
	}
	if result.Metrics["registered_agents"] != 200 {
		t.Fatalf("expected 200 agents, got %+v", result.Metrics)
	}
	if result.Metrics["concurrent_acknowledgements"] != result.Metrics["rollout_targets"] {
		t.Fatalf("expected all targets acknowledged, got %+v", result.Metrics)
	}
	if result.Timings["concurrent_ack_duration"] <= 0 {
		t.Fatalf("expected acknowledgement timing, got %+v", result.Timings)
	}
}

func TestRollbackPropagationTimingScenario(t *testing.T) {
	result, err := RunRollbackPropagationTimingScenario(t.Context(), Options{TempDir: t.TempDir(), Concurrency: 24})
	if err != nil {
		t.Fatalf("run scenario: %v", err)
	}
	if !result.Passed {
		t.Fatalf("expected scenario to pass: %+v", result)
	}
	if result.Metrics["rollback_verification_acknowledgements"] != result.Metrics["candidate_targets"] {
		t.Fatalf("expected rollback verification for all targets, got %+v", result.Metrics)
	}
	if result.Timings["rollback_propagation_duration"] <= 0 {
		t.Fatalf("expected rollback propagation timing, got %+v", result.Timings)
	}
}

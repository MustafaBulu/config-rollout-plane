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

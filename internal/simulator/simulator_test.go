package simulator

import (
	"context"
	"testing"
	"time"

	"config-rollout-plane/internal/domain"
	"config-rollout-plane/internal/rollout"
)

func TestRunCompletesRolloutWithVirtualAgents(t *testing.T) {
	result, err := Run(context.Background(), Options{
		AgentCount:  1000,
		Concurrency: 64,
		Service:     "payment-service",
		Environment: domain.EnvironmentProduction,
	})
	if err != nil {
		t.Fatalf("run simulator: %v", err)
	}
	if result.RegisteredAgents != 1000 {
		t.Fatalf("expected 1000 agents, got %d", result.RegisteredAgents)
	}
	if result.FinalState != string(rollout.StateCompleted) {
		t.Fatalf("expected completed rollout, got %s", result.FinalState)
	}
	if len(result.StageResults) != 3 {
		t.Fatalf("expected 3 stage results, got %+v", result.StageResults)
	}
	if result.SnapshotReads < 3000 {
		t.Fatalf("expected at least one snapshot read per agent per stage, got %d", result.SnapshotReads)
	}
	if result.Acknowledgements < 1000 {
		t.Fatalf("expected acknowledgements for 100 percent stage, got %d", result.Acknowledgements)
	}
}

func TestLatencySummary(t *testing.T) {
	values := []int{5, 1, 3, 2, 4}
	durations := makeDurations(values)
	got := summarize(durations)
	if got.Count != 5 || got.Min != durations[1] || got.Max != durations[0] {
		t.Fatalf("unexpected summary %+v", got)
	}
}

func makeDurations(values []int) []time.Duration {
	durations := make([]time.Duration, 0, len(values))
	for _, value := range values {
		durations = append(durations, time.Duration(value)*time.Millisecond)
	}
	return durations
}

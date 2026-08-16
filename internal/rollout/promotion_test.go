package rollout

import (
	"testing"
	"time"
)

func TestEvaluateStageProgressPromotesWhenCoverageAndMinimumDurationPass(t *testing.T) {
	started := time.Date(2026, 8, 16, 10, 0, 0, 0, time.UTC)
	targets := []StageTarget{
		{AgentID: "agent-1", Status: TargetAcked},
		{AgentID: "agent-2", Status: TargetAcked},
	}

	decision := EvaluateStageProgress(StageProgressInput{
		Targets:               targets,
		RequiredAckPercentage: 95,
		StageStartedAt:        started,
		Now:                   started.Add(61 * time.Second),
		MinimumDuration:       time.Minute,
		DeploymentTimeout:     90 * time.Second,
	})

	if decision != DecisionPromote {
		t.Fatalf("expected promote, got %s", decision)
	}
}

func TestEvaluateStageProgressWaitsUntilMinimumDurationPasses(t *testing.T) {
	started := time.Date(2026, 8, 16, 10, 0, 0, 0, time.UTC)
	targets := []StageTarget{
		{AgentID: "agent-1", Status: TargetAcked},
		{AgentID: "agent-2", Status: TargetAcked},
	}

	decision := EvaluateStageProgress(StageProgressInput{
		Targets:               targets,
		RequiredAckPercentage: 95,
		StageStartedAt:        started,
		Now:                   started.Add(30 * time.Second),
		MinimumDuration:       time.Minute,
		DeploymentTimeout:     90 * time.Second,
	})

	if decision != DecisionWait {
		t.Fatalf("expected wait, got %s", decision)
	}
}

func TestEvaluateStageProgressRollsBackWhenDeploymentTimeoutPassesWithoutCoverage(t *testing.T) {
	started := time.Date(2026, 8, 16, 10, 0, 0, 0, time.UTC)
	targets := []StageTarget{
		{AgentID: "agent-1", Status: TargetAcked},
		{AgentID: "agent-2", Status: TargetPending},
	}

	decision := EvaluateStageProgress(StageProgressInput{
		Targets:               targets,
		RequiredAckPercentage: 95,
		StageStartedAt:        started,
		Now:                   started.Add(91 * time.Second),
		MinimumDuration:       time.Minute,
		DeploymentTimeout:     90 * time.Second,
	})

	if decision != DecisionRollback {
		t.Fatalf("expected rollback, got %s", decision)
	}
}

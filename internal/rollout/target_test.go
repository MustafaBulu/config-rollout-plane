package rollout

import (
	"fmt"
	"testing"
	"time"

	"config-rollout-plane/internal/domain"
)

func TestBuildStageTargetsFreezesEligibleAgents(t *testing.T) {
	agents := testAgents(200)
	rollout := Rollout{
		ID:                 "rollout-1",
		ConfigDefinitionID: "cfg-1",
		CandidateVersionID: "ver-2",
	}
	stage := Stage{ID: "stage-5", Percentage: 5}

	targets, err := BuildStageTargets(BuildTargetsInput{
		Rollout:          rollout,
		Stage:            stage,
		Agents:           agents,
		SnapshotRevision: 1,
		CreatedAt:        time.Now(),
	})
	if err != nil {
		t.Fatalf("build targets: %v", err)
	}
	if len(targets) == 0 {
		t.Fatal("expected at least one target")
	}

	for _, target := range targets {
		if !Eligible(target.Bucket, 5) {
			t.Fatalf("target %s has ineligible bucket %d", target.AgentID, target.Bucket)
		}
		if target.ExpectedVersionID != "ver-2" {
			t.Fatalf("expected candidate version ver-2, got %q", target.ExpectedVersionID)
		}
	}

	agents = append(agents, domain.Agent{ID: "new-agent"})
	if containsTarget(targets, "new-agent") {
		t.Fatal("new agent should not appear in an already frozen target cohort")
	}
}

func TestBuildStageTargetsIncreasingPercentageKeepsEarlierTargets(t *testing.T) {
	agents := testAgents(500)
	rollout := Rollout{
		ID:                 "rollout-1",
		ConfigDefinitionID: "cfg-1",
		CandidateVersionID: "ver-2",
	}

	targets5, err := BuildStageTargets(BuildTargetsInput{
		Rollout:          rollout,
		Stage:            Stage{ID: "stage-5", Percentage: 5},
		Agents:           agents,
		SnapshotRevision: 1,
		CreatedAt:        time.Now(),
	})
	if err != nil {
		t.Fatalf("build 5 percent targets: %v", err)
	}
	targets25, err := BuildStageTargets(BuildTargetsInput{
		Rollout:          rollout,
		Stage:            Stage{ID: "stage-25", Percentage: 25},
		Agents:           agents,
		SnapshotRevision: 2,
		CreatedAt:        time.Now(),
	})
	if err != nil {
		t.Fatalf("build 25 percent targets: %v", err)
	}

	for _, target := range targets5 {
		if !containsTarget(targets25, target.AgentID) {
			t.Fatalf("agent %s was in 5 percent cohort but not in 25 percent cohort", target.AgentID)
		}
	}
}

func TestApplyAckCountsOnlyCurrentStageSnapshotAndVersion(t *testing.T) {
	targets := []StageTarget{
		{
			RolloutID:         "rollout-1",
			StageID:           "stage-5",
			AgentID:           "agent-1",
			ExpectedVersionID: "ver-2",
			SnapshotRevision:  7,
			Status:            TargetPending,
		},
	}

	counted, err := ApplyAck(targets, Ack{
		RolloutID:        "rollout-1",
		StageID:          "stage-previous",
		AgentID:          "agent-1",
		VersionID:        "ver-2",
		SnapshotRevision: 7,
		AckedAt:          time.Now(),
	})
	if err != nil {
		t.Fatalf("apply late ack: %v", err)
	}
	if counted {
		t.Fatal("late ack from previous stage should not be counted")
	}

	counted, err = ApplyAck(targets, Ack{
		RolloutID:        "rollout-1",
		StageID:          "stage-5",
		AgentID:          "agent-1",
		VersionID:        "ver-2",
		SnapshotRevision: 7,
		AckedAt:          time.Now(),
	})
	if err != nil {
		t.Fatalf("apply current ack: %v", err)
	}
	if !counted {
		t.Fatal("current ack should be counted")
	}

	coverage := AcknowledgementCoverage(targets)
	if coverage.Total != 1 || coverage.Acked != 1 || coverage.Percentage != 100 {
		t.Fatalf("unexpected coverage: %+v", coverage)
	}
}

func TestCoverageDoesNotShrinkWhenAgentStopsHeartbeating(t *testing.T) {
	targets := []StageTarget{
		{AgentID: "agent-1", Status: TargetAcked},
		{AgentID: "agent-2", Status: TargetPending},
	}

	coverage := AcknowledgementCoverage(targets)
	if coverage.Total != 2 {
		t.Fatalf("expected frozen denominator 2, got %d", coverage.Total)
	}
	if coverage.Acked != 1 {
		t.Fatalf("expected one ack, got %d", coverage.Acked)
	}
}

func testAgents(count int) []domain.Agent {
	agents := make([]domain.Agent, 0, count)
	for i := range count {
		agents = append(agents, domain.Agent{ID: "agent-" + threeDigits(i)})
	}
	return agents
}

func threeDigits(value int) string {
	return fmt.Sprintf("%03d", value)
}

func containsTarget(targets []StageTarget, agentID string) bool {
	for _, target := range targets {
		if target.AgentID == agentID {
			return true
		}
	}
	return false
}

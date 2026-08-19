package rollout

import "testing"

func TestEffectiveVersionReturnsCandidateForCurrentStageTarget(t *testing.T) {
	r := Rollout{
		ID:                 "rollout-1",
		ConfigDefinitionID: "cfg-1",
		StableVersionID:    "ver-1",
		CandidateVersionID: "ver-2",
		State:              StateDeploying,
		CurrentStageID:     "stage-5",
	}
	targets := []StageTarget{
		{
			RolloutID:         "rollout-1",
			StageID:           "stage-5",
			AgentID:           "agent-1",
			ExpectedVersionID: "ver-2",
			SnapshotRevision:  3,
		},
	}

	got := EffectiveVersionForAgent(r, targets, "agent-1")
	if got.VersionID != "ver-2" {
		t.Fatalf("expected candidate version, got %q", got.VersionID)
	}
	if got.AssignedRollout != "rollout-1" || got.AssignedStage != "stage-5" {
		t.Fatalf("expected rollout assignment, got %+v", got)
	}
}

func TestEffectiveVersionReturnsStableForAgentOutsideFrozenTargets(t *testing.T) {
	r := Rollout{
		ID:                 "rollout-1",
		StableVersionID:    "ver-1",
		CandidateVersionID: "ver-2",
		State:              StateDeploying,
		CurrentStageID:     "stage-5",
	}
	targets := []StageTarget{
		{
			RolloutID:         "rollout-1",
			StageID:           "stage-5",
			AgentID:           "agent-1",
			ExpectedVersionID: "ver-2",
			SnapshotRevision:  3,
		},
	}

	got := EffectiveVersionForAgent(r, targets, "new-agent")
	if got.VersionID != "ver-1" {
		t.Fatalf("expected stable version for new agent, got %q", got.VersionID)
	}
	if got.AssignedRollout != "" || got.AssignedStage != "" {
		t.Fatalf("stable assignment should not carry rollout metadata: %+v", got)
	}
}

func TestEffectiveVersionIgnoresPreviousStageTargets(t *testing.T) {
	r := Rollout{
		ID:                 "rollout-1",
		StableVersionID:    "ver-1",
		CandidateVersionID: "ver-2",
		State:              StateDeploying,
		CurrentStageID:     "stage-25",
	}
	targets := []StageTarget{
		{
			RolloutID:         "rollout-1",
			StageID:           "stage-5",
			AgentID:           "agent-1",
			ExpectedVersionID: "ver-2",
			SnapshotRevision:  3,
		},
	}

	got := EffectiveVersionForAgent(r, targets, "agent-1")
	if got.VersionID != "ver-1" {
		t.Fatalf("expected stable version for previous-stage-only target, got %q", got.VersionID)
	}
}

func TestEffectiveVersionStopsServingCandidateDuringRollback(t *testing.T) {
	r := Rollout{
		ID:                 "rollout-1",
		StableVersionID:    "ver-1",
		CandidateVersionID: "ver-2",
		State:              StateRollingBack,
		CurrentStageID:     "stage-5",
	}
	targets := []StageTarget{
		{
			RolloutID:         "rollout-1",
			StageID:           "stage-5",
			AgentID:           "agent-1",
			ExpectedVersionID: "ver-2",
			SnapshotRevision:  3,
		},
	}

	got := EffectiveVersionForAgent(r, targets, "agent-1")
	if got.VersionID != "ver-1" {
		t.Fatalf("expected stable version during rollback, got %q", got.VersionID)
	}
}

func TestEffectiveVersionAssignsStableForRollbackVerificationTarget(t *testing.T) {
	stageID := rollbackVerificationStageID("rollout-1")
	r := Rollout{
		ID:                 "rollout-1",
		StableVersionID:    "ver-1",
		CandidateVersionID: "ver-2",
		State:              StateRollingBack,
		CurrentStageID:     stageID,
	}
	targets := []StageTarget{
		{
			RolloutID:         "rollout-1",
			StageID:           stageID,
			AgentID:           "agent-1",
			ExpectedVersionID: "ver-1",
			SnapshotRevision:  4,
		},
	}

	got := EffectiveVersionForAgent(r, targets, "agent-1")
	if got.VersionID != "ver-1" {
		t.Fatalf("expected stable version during rollback verification, got %q", got.VersionID)
	}
	if got.AssignedRollout != "rollout-1" || got.AssignedStage != stageID {
		t.Fatalf("expected rollback verification assignment, got %+v", got)
	}
	if got.SnapshotRevision != 4 {
		t.Fatalf("expected rollback verification revision, got %d", got.SnapshotRevision)
	}
}

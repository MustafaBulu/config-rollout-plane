package rollout

type EffectiveVersion struct {
	VersionID        string
	AssignedRollout  string
	AssignedStage    string
	SnapshotRevision int64
}

func EffectiveVersionForAgent(rollout Rollout, targets []StageTarget, agentID string) EffectiveVersion {
	stable := EffectiveVersion{VersionID: rollout.StableVersionID}

	if rollout.State == StateRollingBack {
		return rollbackVerificationVersionForAgent(rollout, targets, agentID)
	}

	if !servesCandidate(rollout.State) {
		return stable
	}

	for _, target := range targets {
		if target.AgentID != agentID {
			continue
		}
		if target.RolloutID != rollout.ID || target.StageID != rollout.CurrentStageID {
			continue
		}
		return EffectiveVersion{
			VersionID:        target.ExpectedVersionID,
			AssignedRollout:  target.RolloutID,
			AssignedStage:    target.StageID,
			SnapshotRevision: target.SnapshotRevision,
		}
	}

	return stable
}

func rollbackVerificationVersionForAgent(rollout Rollout, targets []StageTarget, agentID string) EffectiveVersion {
	stable := EffectiveVersion{VersionID: rollout.StableVersionID}
	for _, target := range targets {
		if target.AgentID != agentID {
			continue
		}
		if target.RolloutID != rollout.ID || target.StageID != rollout.CurrentStageID {
			continue
		}
		if target.ExpectedVersionID != rollout.StableVersionID {
			return stable
		}
		return EffectiveVersion{
			VersionID:        rollout.StableVersionID,
			AssignedRollout:  target.RolloutID,
			AssignedStage:    target.StageID,
			SnapshotRevision: target.SnapshotRevision,
		}
	}
	return stable
}

func servesCandidate(state State) bool {
	switch state {
	case StateDeploying, StateEvaluating, StatePromoting, StatePaused:
		return true
	default:
		return false
	}
}

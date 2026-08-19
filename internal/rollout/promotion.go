package rollout

import "time"

type StageDecision string

const (
	DecisionWait     StageDecision = "WAIT"
	DecisionPromote  StageDecision = "PROMOTE"
	DecisionPause    StageDecision = "PAUSE"
	DecisionRollback StageDecision = "ROLLBACK"
)

type StageProgressInput struct {
	Targets               []StageTarget
	RequiredAckPercentage float64
	StageStartedAt        time.Time
	Now                   time.Time
	MinimumDuration       time.Duration
	DeploymentTimeout     time.Duration
}

func EvaluateStageProgress(input StageProgressInput) StageDecision {
	elapsed := input.Now.Sub(input.StageStartedAt)
	coverageReached := CoverageReached(input.Targets, input.RequiredAckPercentage)

	if coverageReached && elapsed >= input.MinimumDuration {
		return DecisionPromote
	}
	if !coverageReached && input.DeploymentTimeout > 0 && elapsed >= input.DeploymentTimeout {
		return DecisionRollback
	}
	return DecisionWait
}

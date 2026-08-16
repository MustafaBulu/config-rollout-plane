package rollout

import (
	"fmt"
	"slices"
	"time"

	"config-rollout-plane/internal/domain"
)

type TargetStatus string

const (
	TargetPending TargetStatus = "PENDING"
	TargetAcked   TargetStatus = "ACKED"
)

type Rollout struct {
	ID                 string
	ConfigDefinitionID string
	TenantID           string
	ConfigKey          string
	Environment        domain.Environment
	TargetServices     []string
	StableVersionID    string
	CandidateVersionID string
	CandidateVersion   int
	State              State
	CurrentStageID     string
	CurrentStageIndex  int
	RequiredAckPercent float64
	StageStartedAt     time.Time
	DeploymentTimeout  time.Duration
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type StageTarget struct {
	RolloutID         string
	StageID           string
	AgentID           string
	Bucket            int
	ExpectedVersionID string
	SnapshotRevision  int64
	CreatedAt         time.Time
	AckedAt           time.Time
	Status            TargetStatus
}

type BuildTargetsInput struct {
	Rollout          Rollout
	Stage            Stage
	Agents           []domain.Agent
	SnapshotRevision int64
	CreatedAt        time.Time
}

func BuildStageTargets(input BuildTargetsInput) ([]StageTarget, error) {
	if input.Rollout.ID == "" {
		return nil, fmt.Errorf("rollout id is required")
	}
	if input.Rollout.ConfigDefinitionID == "" {
		return nil, fmt.Errorf("config definition id is required")
	}
	if input.Rollout.CandidateVersionID == "" {
		return nil, fmt.Errorf("candidate version id is required")
	}
	if input.Stage.ID == "" {
		return nil, fmt.Errorf("stage id is required")
	}
	if input.SnapshotRevision < 1 {
		return nil, fmt.Errorf("snapshot revision must be positive")
	}

	targets := make([]StageTarget, 0, len(input.Agents))
	for _, agent := range input.Agents {
		bucket, err := Bucket(AssignmentKey{
			ConfigDefinitionID: input.Rollout.ConfigDefinitionID,
			CandidateVersionID: input.Rollout.CandidateVersionID,
			AgentID:            agent.ID,
		})
		if err != nil {
			return nil, err
		}
		if !Eligible(bucket, input.Stage.Percentage) {
			continue
		}

		targets = append(targets, StageTarget{
			RolloutID:         input.Rollout.ID,
			StageID:           input.Stage.ID,
			AgentID:           agent.ID,
			Bucket:            bucket,
			ExpectedVersionID: input.Rollout.CandidateVersionID,
			SnapshotRevision:  input.SnapshotRevision,
			CreatedAt:         input.CreatedAt,
			Status:            TargetPending,
		})
	}

	slices.SortFunc(targets, func(a, b StageTarget) int {
		if a.Bucket < b.Bucket {
			return -1
		}
		if a.Bucket > b.Bucket {
			return 1
		}
		if a.AgentID < b.AgentID {
			return -1
		}
		if a.AgentID > b.AgentID {
			return 1
		}
		return 0
	})

	return targets, nil
}

type Ack struct {
	RolloutID        string
	StageID          string
	AgentID          string
	VersionID        string
	SnapshotRevision int64
	AckedAt          time.Time
}

func ApplyAck(targets []StageTarget, ack Ack) (bool, error) {
	for i := range targets {
		target := &targets[i]
		if target.AgentID != ack.AgentID {
			continue
		}

		if target.RolloutID != ack.RolloutID ||
			target.StageID != ack.StageID ||
			target.ExpectedVersionID != ack.VersionID ||
			target.SnapshotRevision != ack.SnapshotRevision {
			return false, nil
		}

		if target.Status == TargetAcked {
			return false, nil
		}

		target.Status = TargetAcked
		target.AckedAt = ack.AckedAt
		return true, nil
	}

	return false, nil
}

type Coverage struct {
	Total      int
	Acked      int
	Percentage float64
}

func AcknowledgementCoverage(targets []StageTarget) Coverage {
	coverage := Coverage{Total: len(targets)}
	for _, target := range targets {
		if target.Status == TargetAcked {
			coverage.Acked++
		}
	}
	if coverage.Total > 0 {
		coverage.Percentage = float64(coverage.Acked) / float64(coverage.Total) * 100
	}
	return coverage
}

func CoverageReached(targets []StageTarget, requiredPercentage float64) bool {
	coverage := AcknowledgementCoverage(targets)
	if coverage.Total == 0 {
		return false
	}
	return coverage.Percentage >= requiredPercentage
}

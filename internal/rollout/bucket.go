package rollout

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
)

const BucketCount = 10000

type AssignmentKey struct {
	ConfigDefinitionID string
	CandidateVersionID string
	AgentID            string
}

func Bucket(key AssignmentKey) (int, error) {
	if key.ConfigDefinitionID == "" {
		return 0, fmt.Errorf("config definition id is required")
	}
	if key.CandidateVersionID == "" {
		return 0, fmt.Errorf("candidate version id is required")
	}
	if key.AgentID == "" {
		return 0, fmt.Errorf("agent id is required")
	}

	sum := sha256.Sum256([]byte(key.ConfigDefinitionID + "\x00" + key.CandidateVersionID + "\x00" + key.AgentID))
	number := binary.BigEndian.Uint64(sum[:8])
	return int(number % BucketCount), nil
}

func Eligible(bucket int, percentage int) bool {
	if percentage <= 0 {
		return false
	}
	if percentage >= 100 {
		return bucket >= 0 && bucket < BucketCount
	}
	return bucket >= 0 && bucket < percentage*100
}

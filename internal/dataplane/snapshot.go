package dataplane

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

type SnapshotStore interface {
	GetSnapshot(ctx context.Context, agentID string) (Snapshot, error)
}

type Snapshot struct {
	Revision    int64          `json:"revision"`
	GeneratedAt time.Time      `json:"generatedAt"`
	Configs     []SnapshotItem `json:"configs"`
}

type SnapshotItem struct {
	ConfigDefinitionID string          `json:"configDefinitionId"`
	Key                string          `json:"key"`
	VersionID          string          `json:"versionId"`
	Version            int             `json:"version"`
	Value              json.RawMessage `json:"value"`
	Checksum           string          `json:"checksum"`
	Assignment         Assignment      `json:"assignment"`
}

type Assignment struct {
	RolloutID string `json:"rolloutId,omitempty"`
	StageID   string `json:"stageId,omitempty"`
}

func (s Snapshot) ETag() (string, error) {
	payload, err := json.Marshal(s)
	if err != nil {
		return "", err
	}

	sum := sha256.Sum256(payload)
	return fmt.Sprintf(`"snapshot-%s"`, hex.EncodeToString(sum[:])), nil
}

func Checksum(value json.RawMessage) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

func CloneSnapshot(snapshot Snapshot) Snapshot {
	cloned := snapshot
	cloned.Configs = make([]SnapshotItem, len(snapshot.Configs))
	for i, item := range snapshot.Configs {
		cloned.Configs[i] = item
		cloned.Configs[i].Value = cloneRaw(item.Value)
	}
	return cloned
}

func cloneRaw(value json.RawMessage) json.RawMessage {
	if len(value) == 0 {
		return nil
	}
	cloned := make([]byte, len(value))
	copy(cloned, value)
	return cloned
}

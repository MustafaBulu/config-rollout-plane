package dataplane

import (
	"context"
	"sync"
)

type MemorySnapshotStore struct {
	mu        sync.RWMutex
	snapshots map[string]Snapshot
}

func NewMemorySnapshotStore() *MemorySnapshotStore {
	return &MemorySnapshotStore{snapshots: make(map[string]Snapshot)}
}

func (s *MemorySnapshotStore) PutSnapshot(agentID string, snapshot Snapshot) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.snapshots[agentID] = CloneSnapshot(snapshot)
}

func (s *MemorySnapshotStore) GetSnapshot(ctx context.Context, agentID string) (Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	snapshot, ok := s.snapshots[agentID]
	if !ok {
		return Snapshot{}, ErrSnapshotNotFound
	}
	return CloneSnapshot(snapshot), nil
}

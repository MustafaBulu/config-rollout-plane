package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"config-rollout-plane/internal/dataplane"
)

var (
	ErrNoSnapshot     = errors.New("no snapshot is available")
	ErrConfigNotFound = errors.New("config not found")
	ErrBadChecksum    = errors.New("snapshot checksum is invalid")
)

type SnapshotCache struct {
	mu       sync.RWMutex
	path     string
	snapshot *dataplane.Snapshot
}

func NewSnapshotCache(path string) *SnapshotCache {
	return &SnapshotCache{path: path}
}

func (c *SnapshotCache) Load(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	data, err := os.ReadFile(c.path)
	if err != nil {
		return err
	}

	var snapshot dataplane.Snapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return err
	}
	if err := ValidateSnapshot(snapshot); err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	cloned := dataplane.CloneSnapshot(snapshot)
	c.snapshot = &cloned
	return nil
}

func (c *SnapshotCache) Save(ctx context.Context, snapshot dataplane.Snapshot) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := ValidateSnapshot(snapshot); err != nil {
		return err
	}

	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		return err
	}

	tmp := c.path + ".tmp"
	file, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	previous := previousPath(c.path)
	_ = os.Remove(previous)
	if _, err := os.Stat(c.path); err == nil {
		if err := os.Rename(c.path, previous); err != nil {
			return err
		}
	}
	if err := os.Rename(tmp, c.path); err != nil {
		_ = os.Rename(previous, c.path)
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	cloned := dataplane.CloneSnapshot(snapshot)
	c.snapshot = &cloned
	return nil
}

func previousPath(path string) string {
	extension := filepath.Ext(path)
	if extension == "" {
		return path + ".previous"
	}
	return strings.TrimSuffix(path, extension) + ".previous" + extension
}

func (c *SnapshotCache) Snapshot() (dataplane.Snapshot, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.snapshot == nil {
		return dataplane.Snapshot{}, ErrNoSnapshot
	}
	return dataplane.CloneSnapshot(*c.snapshot), nil
}

func (c *SnapshotCache) Config(key string) (dataplane.SnapshotItem, error) {
	snapshot, err := c.Snapshot()
	if err != nil {
		return dataplane.SnapshotItem{}, err
	}

	for _, item := range snapshot.Configs {
		if item.Key == key {
			return item, nil
		}
	}
	return dataplane.SnapshotItem{}, fmt.Errorf("%w: %s", ErrConfigNotFound, key)
}

func (c *SnapshotCache) Check(context.Context) error {
	_, err := c.Snapshot()
	return err
}

func ValidateSnapshot(snapshot dataplane.Snapshot) error {
	if snapshot.Revision < 1 {
		return fmt.Errorf("%w: revision must be positive", ErrNoSnapshot)
	}
	for _, item := range snapshot.Configs {
		if item.Checksum != dataplane.Checksum(item.Value) {
			return fmt.Errorf("%w: %s", ErrBadChecksum, item.Key)
		}
	}
	return nil
}

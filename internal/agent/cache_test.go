package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"config-rollout-plane/internal/dataplane"
)

func TestSnapshotCacheSaveLoadAndValidate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "snapshot.json")
	cache := NewSnapshotCache(path)
	snapshot := testSnapshot()

	if err := cache.Save(context.Background(), snapshot); err != nil {
		t.Fatalf("save snapshot: %v", err)
	}

	loaded := NewSnapshotCache(path)
	if err := loaded.Load(context.Background()); err != nil {
		t.Fatalf("load snapshot: %v", err)
	}

	item, err := loaded.Config("payment.authorization.timeout")
	if err != nil {
		t.Fatalf("get config: %v", err)
	}
	if string(item.Value) != "1500" {
		t.Fatalf("expected value 1500, got %s", item.Value)
	}
}

func TestSnapshotCacheDoesNotReplaceCacheWithBadChecksum(t *testing.T) {
	path := filepath.Join(t.TempDir(), "snapshot.json")
	cache := NewSnapshotCache(path)
	good := testSnapshot()

	if err := cache.Save(context.Background(), good); err != nil {
		t.Fatalf("save good snapshot: %v", err)
	}

	bad := good
	bad.Revision = 2
	bad.Configs[0].Value = []byte(`1600`)
	if err := cache.Save(context.Background(), bad); err == nil {
		t.Fatal("expected bad checksum error")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read cache file: %v", err)
	}
	if string(data) == "" {
		t.Fatal("expected previous cache file to remain")
	}
}

func testSnapshot() dataplane.Snapshot {
	value := []byte(`1500`)
	return dataplane.Snapshot{
		Revision:    1,
		GeneratedAt: time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC),
		Configs: []dataplane.SnapshotItem{
			{
				ConfigDefinitionID: "cfg-1",
				Key:                "payment.authorization.timeout",
				VersionID:          "ver-1",
				Version:            1,
				Value:              value,
				Checksum:           dataplane.Checksum(value),
			},
		},
	}
}

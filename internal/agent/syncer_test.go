package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"config-rollout-plane/internal/dataplane"
)

func TestSyncerFetchesSnapshotAndLocalAPIContinuesFromCache(t *testing.T) {
	store := dataplane.NewMemorySnapshotStore()
	store.PutSnapshot("agent-1", testSnapshot())

	backend := httptest.NewServer(dataplane.NewHandler(store, dataplane.NewStaticCredentialVerifier(map[string]string{
		"token-agent-1": "agent-1",
	})))
	defer backend.Close()

	cache := NewSnapshotCache(filepath.Join(t.TempDir(), "snapshot.json"))
	syncer := &Syncer{
		Client: SnapshotClient{
			BaseURL:    backend.URL,
			AgentID:    "agent-1",
			Token:      "token-agent-1",
			HTTPClient: backend.Client(),
		},
		Cache: cache,
	}

	if err := syncer.SyncOnce(t.Context()); err != nil {
		t.Fatalf("sync once: %v", err)
	}

	local := httptest.NewServer(NewHandler(cache))
	defer local.Close()

	assertLocalConfig(t, local, 1500)

	backend.Close()
	assertLocalConfig(t, local, 1500)
}

func TestSyncerUsesETagForUnchangedSnapshot(t *testing.T) {
	store := dataplane.NewMemorySnapshotStore()
	store.PutSnapshot("agent-1", testSnapshot())

	backend := httptest.NewServer(dataplane.NewHandler(store, dataplane.NewStaticCredentialVerifier(map[string]string{
		"token-agent-1": "agent-1",
	})))
	defer backend.Close()

	cache := NewSnapshotCache(filepath.Join(t.TempDir(), "snapshot.json"))
	syncer := &Syncer{
		Client: SnapshotClient{
			BaseURL:    backend.URL,
			AgentID:    "agent-1",
			Token:      "token-agent-1",
			HTTPClient: backend.Client(),
		},
		Cache: cache,
	}

	if err := syncer.SyncOnce(t.Context()); err != nil {
		t.Fatalf("first sync: %v", err)
	}
	if err := syncer.SyncOnce(t.Context()); err != nil {
		t.Fatalf("second sync should handle 304: %v", err)
	}
}

func TestSyncerAcknowledgesSnapshotItems(t *testing.T) {
	store := dataplane.NewMemorySnapshotStore()
	store.PutSnapshot("agent-1", testSnapshot())

	backend := httptest.NewServer(dataplane.NewHandler(store, dataplane.NewStaticCredentialVerifier(map[string]string{
		"token-agent-1": "agent-1",
	})))
	defer backend.Close()

	ack := &recordingAcknowledger{}
	syncer := &Syncer{
		Client: SnapshotClient{
			BaseURL:    backend.URL,
			AgentID:    "agent-1",
			Token:      "token-agent-1",
			HTTPClient: backend.Client(),
		},
		Cache:        NewSnapshotCache(filepath.Join(t.TempDir(), "snapshot.json")),
		Acknowledger: ack,
	}

	if err := syncer.SyncOnce(t.Context()); err != nil {
		t.Fatalf("sync once: %v", err)
	}
	if ack.count != 1 {
		t.Fatalf("expected one acknowledgement, got %d", ack.count)
	}
	if ack.revision != 1 {
		t.Fatalf("expected revision 1, got %d", ack.revision)
	}
}

func assertLocalConfig(t *testing.T, server *httptest.Server, want int) {
	t.Helper()

	resp, err := server.Client().Get(server.URL + "/v1/config/payment.authorization.timeout")
	if err != nil {
		t.Fatalf("get local config: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	var body struct {
		Key     string          `json:"key"`
		Version int             `json:"version"`
		Value   json.RawMessage `json:"value"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode local config: %v", err)
	}
	if string(body.Value) != "1500" {
		t.Fatalf("expected value %d, got %s", want, body.Value)
	}
}

type recordingAcknowledger struct {
	count    int
	revision int64
}

func (a *recordingAcknowledger) Acknowledge(ctx context.Context, agentID string, token string, item dataplane.SnapshotItem, revision int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	a.count++
	a.revision = revision
	return nil
}

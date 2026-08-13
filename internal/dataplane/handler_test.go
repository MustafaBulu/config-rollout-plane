package dataplane

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHandlerReturnsSnapshotWithETag(t *testing.T) {
	handler := newSnapshotTestHandler()

	req := httptest.NewRequest(http.MethodGet, "/v1/agents/agent-1/snapshot", nil)
	req.Header.Set("Authorization", "Bearer token-agent-1")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusOK, rec.Code, rec.Body.String())
	}
	if rec.Header().Get("ETag") == "" {
		t.Fatal("expected ETag header")
	}

	var snapshot Snapshot
	if err := json.Unmarshal(rec.Body.Bytes(), &snapshot); err != nil {
		t.Fatalf("decode snapshot: %v", err)
	}
	if snapshot.Revision != 1 {
		t.Fatalf("expected revision 1, got %d", snapshot.Revision)
	}
	if len(snapshot.Configs) != 1 {
		t.Fatalf("expected one config, got %d", len(snapshot.Configs))
	}
}

func TestHandlerReturnsNotModifiedWhenETagMatches(t *testing.T) {
	handler := newSnapshotTestHandler()

	req := httptest.NewRequest(http.MethodGet, "/v1/agents/agent-1/snapshot", nil)
	req.Header.Set("Authorization", "Bearer token-agent-1")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	etag := rec.Header().Get("ETag")

	req = httptest.NewRequest(http.MethodGet, "/v1/agents/agent-1/snapshot", nil)
	req.Header.Set("Authorization", "Bearer token-agent-1")
	req.Header.Set("If-None-Match", etag)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotModified {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusNotModified, rec.Code, rec.Body.String())
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("expected empty 304 body, got %s", rec.Body.String())
	}
}

func TestHandlerRejectsCredentialSubjectMismatch(t *testing.T) {
	handler := newSnapshotTestHandler()

	req := httptest.NewRequest(http.MethodGet, "/v1/agents/agent-2/snapshot", nil)
	req.Header.Set("Authorization", "Bearer token-agent-1")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusForbidden, rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "forbidden") {
		t.Fatalf("expected forbidden response, got %s", rec.Body.String())
	}
}

func newSnapshotTestHandler() http.Handler {
	store := NewMemorySnapshotStore()
	store.PutSnapshot("agent-1", Snapshot{
		Revision:    1,
		GeneratedAt: time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC),
		Configs: []SnapshotItem{
			{
				ConfigDefinitionID: "cfg-1",
				Key:                "payment.authorization.timeout",
				VersionID:          "ver-1",
				Version:            1,
				Value:              []byte(`1500`),
				Checksum:           Checksum([]byte(`1500`)),
			},
		},
	})

	return NewHandler(store, NewStaticCredentialVerifier(map[string]string{
		"token-agent-1": "agent-1",
	}))
}

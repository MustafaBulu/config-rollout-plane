package agent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"config-rollout-plane/internal/dataplane"
)

func TestControlPlaneAcknowledgerSendsRolloutAssignment(t *testing.T) {
	var payload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	acknowledger := ControlPlaneAcknowledger{
		BaseURL:    server.URL,
		HTTPClient: server.Client(),
	}
	err := acknowledger.Acknowledge(t.Context(), "agent-1", "token", dataplane.SnapshotItem{
		ConfigDefinitionID: "cfg-1",
		VersionID:          "ver-2",
		Assignment: dataplane.Assignment{
			RolloutID: "rollout-1",
			StageID:   "stage-5",
		},
	}, 7)
	if err != nil {
		t.Fatalf("acknowledge: %v", err)
	}

	if payload["rollout_id"] != "rollout-1" {
		t.Fatalf("expected rollout_id, got %+v", payload)
	}
	if payload["stage_id"] != "stage-5" {
		t.Fatalf("expected stage_id, got %+v", payload)
	}
	if payload["snapshot_revision"] != float64(7) {
		t.Fatalf("expected snapshot_revision 7, got %+v", payload)
	}
}

func TestRegistrationClientRegistersAgent(t *testing.T) {
	var payload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/agents/register" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"agent":{"id":"agent-1"},"instance_credential":"cred-1"}`))
	}))
	defer server.Close()

	client := RegistrationClient{BaseURL: server.URL, HTTPClient: server.Client()}
	result, err := client.Register(t.Context(), RegisterInput{
		BootstrapToken: "bootstrap",
		ID:             "agent-1",
		Service:        "payment-service",
		Environment:    "production",
		Instance:       "pod-1",
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	if result.AgentID != "agent-1" || result.InstanceCredential != "cred-1" {
		t.Fatalf("unexpected result %+v", result)
	}
	if payload["bootstrap_token"] != "bootstrap" || payload["service"] != "payment-service" {
		t.Fatalf("unexpected payload %+v", payload)
	}
}

package controlplane

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"config-rollout-plane/internal/agentregistry"
	"config-rollout-plane/internal/configregistry"
)

func TestHandlerAgentRegistrationHeartbeatAndAcknowledgement(t *testing.T) {
	handler := NewHandlerWithReadiness(
		configregistry.NewService(configregistry.NewMemoryStore(), configregistry.JSONSchemaValidator{}),
		agentregistry.NewService(agentregistry.NewMemoryStore(), "bootstrap-secret", time.Hour),
		nil,
	)

	var registration registerAgentResponse
	assertJSONRequest(t, handler, http.MethodPost, "/v1/agents/register", `{
		"bootstrap_token": "bootstrap-secret",
		"id": "agent-1",
		"service": "payment-api",
		"environment": "production",
		"instance": "payment-api-1"
	}`, http.StatusCreated, &registration)
	if registration.InstanceCredential == "" {
		t.Fatal("expected instance credential")
	}

	assertRequestWithToken(t, handler, http.MethodPost, "/v1/agents/agent-1/heartbeat", "", registration.InstanceCredential, http.StatusOK)

	var ack acknowledgementResponse
	assertJSONRequestWithToken(t, handler, http.MethodPost, "/v1/agents/agent-1/acknowledgements", `{
		"config_definition_id": "cfg-1",
		"version_id": "ver-1",
		"snapshot_revision": 1
	}`, registration.InstanceCredential, http.StatusCreated, &ack)
	if !ack.Counted {
		t.Fatal("expected acknowledgement to be counted")
	}
}

func TestHandlerAgentCredentialMismatchReturnsForbidden(t *testing.T) {
	handler := NewHandlerWithReadiness(
		configregistry.NewService(configregistry.NewMemoryStore(), configregistry.JSONSchemaValidator{}),
		agentregistry.NewService(agentregistry.NewMemoryStore(), "bootstrap-secret", time.Hour),
		nil,
	)

	var registration registerAgentResponse
	assertJSONRequest(t, handler, http.MethodPost, "/v1/agents/register", `{
		"bootstrap_token": "bootstrap-secret",
		"id": "agent-1",
		"service": "payment-api",
		"environment": "production",
		"instance": "payment-api-1"
	}`, http.StatusCreated, &registration)

	body := assertRequestWithToken(t, handler, http.MethodPost, "/v1/agents/agent-2/heartbeat", "", registration.InstanceCredential, http.StatusForbidden)
	if !strings.Contains(body, "forbidden") {
		t.Fatalf("expected forbidden response, got %s", body)
	}
}

func assertJSONRequestWithToken(t *testing.T, handler http.Handler, method string, path string, body string, token string, wantStatus int, dst any) {
	t.Helper()

	responseBody := assertRequestWithToken(t, handler, method, path, body, token, wantStatus)
	if err := json.Unmarshal([]byte(responseBody), dst); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, responseBody)
	}
}

func assertRequestWithToken(t *testing.T, handler http.Handler, method string, path string, body string, token string, wantStatus int) string {
	t.Helper()

	reqBody := body
	if reqBody == "" {
		reqBody = `{}`
	}
	return assertRequestWithHeaders(t, handler, method, path, reqBody, map[string]string{
		"Authorization": "Bearer " + token,
	}, wantStatus)
}

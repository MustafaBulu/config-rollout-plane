package controlplane

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"config-rollout-plane/internal/configregistry"
)

func TestHandlerConfigRegistryAcceptanceFlow(t *testing.T) {
	handler := newTestHandler()

	assertRequest(t, handler, http.MethodPost, "/v1/tenants", `{
		"id": "payments",
		"name": "Payments"
	}`, http.StatusCreated)

	assertRequest(t, handler, http.MethodPost, "/v1/tenants/payments/configs", `{
		"key": "payment.authorization.timeout",
		"description": "Authorization provider timeout",
		"schema": {"type":"integer","minimum":100,"maximum":10000},
		"default": 2000
	}`, http.StatusCreated)

	var version versionResponse
	assertJSONRequest(t, handler, http.MethodPost, "/v1/tenants/payments/configs/payment.authorization.timeout/versions", `{
		"value": 1500,
		"created_by": "developer@example.com"
	}`, http.StatusCreated, &version)
	if version.VersionNumber != 1 {
		t.Fatalf("expected version number 1, got %d", version.VersionNumber)
	}

	var state environmentStateResponse
	assertJSONRequest(t, handler, http.MethodPost, "/v1/tenants/payments/configs/payment.authorization.timeout/environments/production/stable", `{
		"version_number": 1
	}`, http.StatusOK, &state)
	if state.StableVersionID != version.ID {
		t.Fatalf("expected stable version %q, got %q", version.ID, state.StableVersionID)
	}

	var definition definitionResponse
	assertJSONRequest(t, handler, http.MethodGet, "/v1/tenants/payments/configs/payment.authorization.timeout", "", http.StatusOK, &definition)
	if definition.Key != "payment.authorization.timeout" {
		t.Fatalf("expected config key, got %q", definition.Key)
	}
}

func TestHandlerRejectsVersionOutsideSchema(t *testing.T) {
	handler := newTestHandler()

	assertRequest(t, handler, http.MethodPost, "/v1/tenants", `{
		"id": "payments",
		"name": "Payments"
	}`, http.StatusCreated)

	assertRequest(t, handler, http.MethodPost, "/v1/tenants/payments/configs", `{
		"key": "payment.authorization.timeout",
		"schema": {"type":"integer","minimum":100,"maximum":10000},
		"default": 2000
	}`, http.StatusCreated)

	body := assertRequest(t, handler, http.MethodPost, "/v1/tenants/payments/configs/payment.authorization.timeout/versions", `{
		"value": 20000
	}`, http.StatusBadRequest)
	if !strings.Contains(body, "invalid_input") {
		t.Fatalf("expected invalid_input response, got %s", body)
	}
}

func newTestHandler() http.Handler {
	registry := configregistry.NewService(configregistry.NewMemoryStore(), configregistry.JSONSchemaValidator{})
	return NewHandler(registry)
}

func assertJSONRequest(t *testing.T, handler http.Handler, method string, path string, body string, wantStatus int, dst any) {
	t.Helper()

	responseBody := assertRequest(t, handler, method, path, body, wantStatus)
	if err := json.Unmarshal([]byte(responseBody), dst); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, responseBody)
	}
}

func assertRequest(t *testing.T, handler http.Handler, method string, path string, body string, wantStatus int) string {
	t.Helper()

	var reader *bytes.Reader
	if body == "" {
		reader = bytes.NewReader(nil)
	} else {
		reader = bytes.NewReader([]byte(body))
	}

	req := httptest.NewRequest(method, path, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)
	if rec.Code != wantStatus {
		t.Fatalf("%s %s: expected status %d, got %d with body %s", method, path, wantStatus, rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

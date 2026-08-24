package configcode

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestApplierApplyDocumentsInOrder(t *testing.T) {
	documents := decodeTestDocuments(t, `apiVersion: safeconfig.dev/v1alpha1
kind: Rollout
metadata:
  name: payment-rollout
spec:
  tenant: payments
  key: payment.failure_rate
  environment: production
  candidateVersionRef: payment-failure-rate-v1
---
apiVersion: safeconfig.dev/v1alpha1
kind: Tenant
metadata:
  name: payments
spec:
  name: Payments
---
apiVersion: safeconfig.dev/v1alpha1
kind: ConfigDefinition
metadata:
  name: payment-failure-rate
spec:
  tenant: payments
  key: payment.failure_rate
  schema:
    type: number
    maximum: 1
  default: 0
---
apiVersion: safeconfig.dev/v1alpha1
kind: ConfigVersion
metadata:
  name: payment-failure-rate-v1
spec:
  tenant: payments
  key: payment.failure_rate
  value: 0
---
apiVersion: safeconfig.dev/v1alpha1
kind: StableVersion
metadata:
  name: payment-failure-rate-production
spec:
  tenant: payments
  key: payment.failure_rate
  environment: production
  versionRef: payment-failure-rate-v1
`)

	var calls []string
	versionCreated := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer token-1" {
			t.Fatalf("missing authorization header for %s %s", r.Method, r.URL.Path)
		}
		calls = append(calls, r.Method+" "+r.URL.Path)
		switch r.Method + " " + r.URL.Path {
		case "GET /v1/tenants/payments":
			writeStatus(w, http.StatusNotFound)
		case "POST /v1/tenants":
			expectJSONField(t, r, "id", "payments")
			writeJSON(w, http.StatusCreated, map[string]any{"id": "payments"})
		case "GET /v1/tenants/payments/configs/payment.failure_rate":
			writeStatus(w, http.StatusNotFound)
		case "POST /v1/tenants/payments/configs":
			expectJSONField(t, r, "key", "payment.failure_rate")
			writeJSON(w, http.StatusCreated, map[string]any{"id": "cfg-1"})
		case "GET /v1/tenants/payments/configs/payment.failure_rate/versions":
			if versionCreated {
				writeJSON(w, http.StatusOK, []map[string]any{{"id": "ver-1", "version_number": 1, "value": 0}})
			} else {
				writeJSON(w, http.StatusOK, []map[string]any{})
			}
		case "POST /v1/tenants/payments/configs/payment.failure_rate/versions":
			expectJSONField(t, r, "value", float64(0))
			versionCreated = true
			writeJSON(w, http.StatusCreated, map[string]any{"id": "ver-1", "version_number": 1, "value": 0})
		case "GET /v1/tenants/payments/configs/payment.failure_rate/environments/production/stable":
			writeStatus(w, http.StatusNotFound)
		case "POST /v1/tenants/payments/configs/payment.failure_rate/environments/production/stable":
			expectJSONField(t, r, "version_number", float64(1))
			writeJSON(w, http.StatusOK, map[string]any{"stable_version_id": "ver-1"})
		case "POST /v1/rollouts":
			expectJSONField(t, r, "tenant_id", "payments")
			writeJSON(w, http.StatusCreated, map[string]any{"id": "rollout-1"})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	report, err := Applier{Options: ApplyOptions{
		BaseURL:         server.URL,
		Token:           "token-1",
		IncludeRollouts: true,
		HTTPClient:      server.Client(),
	}}.ApplyDocuments(context.Background(), documents)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(report.Steps) != 5 {
		t.Fatalf("expected 5 steps, got %+v", report.Steps)
	}

	wantCalls := []string{
		"GET /v1/tenants/payments",
		"POST /v1/tenants",
		"GET /v1/tenants/payments/configs/payment.failure_rate",
		"POST /v1/tenants/payments/configs",
		"GET /v1/tenants/payments/configs/payment.failure_rate/versions",
		"POST /v1/tenants/payments/configs/payment.failure_rate/versions",
		"GET /v1/tenants/payments/configs/payment.failure_rate/environments/production/stable",
		"POST /v1/tenants/payments/configs/payment.failure_rate/environments/production/stable",
		"POST /v1/rollouts",
	}
	if !reflect.DeepEqual(calls, wantCalls) {
		t.Fatalf("unexpected calls\n got: %#v\nwant: %#v", calls, wantCalls)
	}
}

func TestApplierSkipsExistingVersionWithMatchingValue(t *testing.T) {
	documents := decodeTestDocuments(t, `apiVersion: safeconfig.dev/v1alpha1
kind: ConfigVersion
metadata:
  name: payment-failure-rate-v1
spec:
  tenant: payments
  key: payment.failure_rate
  value: 0
`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("unexpected write request %s %s", r.Method, r.URL.Path)
		}
		writeJSON(w, http.StatusOK, []map[string]any{{"id": "ver-1", "version_number": 1, "value": 0}})
	}))
	defer server.Close()

	report, err := Applier{Options: ApplyOptions{BaseURL: server.URL, HTTPClient: server.Client()}}.ApplyDocuments(context.Background(), documents)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if report.Steps[0].Status != "skipped" {
		t.Fatalf("expected skipped step, got %+v", report.Steps[0])
	}
}

func TestApplierDryRunDoesNotRequireBaseURL(t *testing.T) {
	documents := decodeTestDocuments(t, `apiVersion: safeconfig.dev/v1alpha1
kind: Tenant
metadata:
  name: payments
spec:
  name: Payments
`)
	report, err := Applier{Options: ApplyOptions{DryRun: true}}.ApplyDocuments(context.Background(), documents)
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if report.Steps[0].Status != "planned" {
		t.Fatalf("expected planned step, got %+v", report.Steps[0])
	}
}

func TestApplierSkipsRolloutWithoutIncludeRollouts(t *testing.T) {
	documents := decodeTestDocuments(t, `apiVersion: safeconfig.dev/v1alpha1
kind: ConfigVersion
metadata:
  name: payment-failure-rate-v1
spec:
  tenant: payments
  key: payment.failure_rate
  value: 0
---
apiVersion: safeconfig.dev/v1alpha1
kind: Rollout
metadata:
  name: payment-rollout
spec:
  tenant: payments
  key: payment.failure_rate
  environment: production
  candidateVersionRef: payment-failure-rate-v1
`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("unexpected write request %s %s", r.Method, r.URL.Path)
		}
		writeJSON(w, http.StatusOK, []map[string]any{{"id": "ver-1", "version_number": 1, "value": 0}})
	}))
	defer server.Close()

	report, err := Applier{Options: ApplyOptions{BaseURL: server.URL, HTTPClient: server.Client()}}.ApplyDocuments(context.Background(), documents)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if report.Steps[1].Status != "skipped" {
		t.Fatalf("expected skipped rollout, got %+v", report.Steps[1])
	}
}

func decodeTestDocuments(t *testing.T, input string) []ManifestDocument {
	t.Helper()
	manifests, err := DecodeManifests(bytes.NewReader([]byte(input)))
	if err != nil {
		t.Fatalf("decode manifests: %v", err)
	}
	documents := make([]ManifestDocument, 0, len(manifests))
	for i, manifest := range manifests {
		documents = append(documents, ManifestDocument{Path: "test.yaml", Document: i + 1, Manifest: manifest})
	}
	return documents
}

func writeStatus(w http.ResponseWriter, status int) {
	w.WriteHeader(status)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func expectJSONField(t *testing.T, r *http.Request, key string, want any) {
	t.Helper()
	var payload map[string]any
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if got := payload[key]; !reflect.DeepEqual(got, want) {
		t.Fatalf("field %s got %#v want %#v in payload %+v", key, got, want, payload)
	}
}

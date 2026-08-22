package configcode

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidatorAcceptsConfigAsCodeFlow(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "payment.yaml")
	writeFile(t, path, `apiVersion: safeconfig.dev/v1alpha1
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
    minimum: 0
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
kind: ConfigVersion
metadata:
  name: payment-failure-rate-v2
spec:
  tenant: payments
  key: payment.failure_rate
  value: 0.2
---
apiVersion: safeconfig.dev/v1alpha1
kind: StableVersion
metadata:
  name: payment-failure-rate-production
spec:
  tenant: payments
  key: payment.failure_rate
  environment: production
  versionNumber: 1
---
apiVersion: safeconfig.dev/v1alpha1
kind: Rollout
metadata:
  name: payment-failure-rate-bad
spec:
  tenant: payments
  key: payment.failure_rate
  environment: production
  candidateVersion: 2
  targetServices:
    - payment-service
  stages:
    - id: stage-5
      percentage: 5
      minimumDurationSeconds: 10
    - id: stage-100
      percentage: 100
      minimumDurationSeconds: 30
  guardrails:
    - name: candidate-error-rate
      query: sum(rate(payment_requests_total[30s]))
      operator: <
      threshold: 0.05
      consecutiveFailures: 2
`)

	report := Validator{}.ValidatePaths([]string{dir})
	if !report.OK() {
		t.Fatalf("expected valid report, got %+v", report.Errors)
	}
	if report.Manifests != 6 {
		t.Fatalf("expected 6 manifests, got %d", report.Manifests)
	}
}

func TestValidatorRejectsValueOutsideSchema(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "payment.yaml")
	writeFile(t, path, `apiVersion: safeconfig.dev/v1alpha1
kind: ConfigDefinition
metadata:
  name: payment-failure-rate
spec:
  tenant: payments
  key: payment.failure_rate
  schema:
    type: number
    maximum: 1
---
apiVersion: safeconfig.dev/v1alpha1
kind: ConfigVersion
metadata:
  name: payment-failure-rate-v1
spec:
  tenant: payments
  key: payment.failure_rate
  value: 2
`)

	report := Validator{}.ValidatePaths([]string{path})
	if report.OK() {
		t.Fatalf("expected validation errors")
	}
	if got := report.Errors[0].Document; got != 2 {
		t.Fatalf("expected document 2 error, got document %d: %+v", got, report.Errors)
	}
}

func TestValidatorRejectsInvalidRolloutStagePlan(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rollout.yaml")
	writeFile(t, path, `apiVersion: safeconfig.dev/v1alpha1
kind: Rollout
metadata:
  name: bad-rollout
spec:
  tenant: payments
  key: payment.failure_rate
  environment: production
  candidateVersion: 1
  stages:
    - id: stage-50
      percentage: 50
`)

	report := Validator{}.ValidatePaths([]string{path})
	if report.OK() {
		t.Fatalf("expected validation errors")
	}
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
}

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunValidate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tenant.yaml")
	if err := os.WriteFile(path, []byte(`apiVersion: safeconfig.dev/v1alpha1
kind: Tenant
metadata:
  name: payments
spec:
  name: Payments
`), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	if err := run([]string{"validate", path}); err != nil {
		t.Fatalf("validate: %v", err)
	}
}

func TestRunRejectsUnknownCommand(t *testing.T) {
	if err := run([]string{"apply"}); err == nil {
		t.Fatalf("expected error")
	}
}

func TestRunApplyDryRun(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tenant.yaml")
	if err := os.WriteFile(path, []byte(`apiVersion: safeconfig.dev/v1alpha1
kind: Tenant
metadata:
  name: payments
spec:
  name: Payments
`), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	if err := run([]string{"apply", "--dry-run", path}); err != nil {
		t.Fatalf("apply dry run: %v", err)
	}
}

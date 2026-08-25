package main

import "testing"

func TestRunSimulatorJSON(t *testing.T) {
	if err := run([]string{"-agents", "50", "-concurrency", "8", "-format", "json"}); err != nil {
		t.Fatalf("run: %v", err)
	}
}

func TestRunRejectsUnknownFormat(t *testing.T) {
	if err := run([]string{"-agents", "10", "-format", "xml"}); err == nil {
		t.Fatalf("expected error")
	}
}

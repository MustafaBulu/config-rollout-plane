package main

import "testing"

func TestRunReliabilityJSON(t *testing.T) {
	if err := run([]string{"-scenario", "control-plane-restart", "-format", "json"}); err != nil {
		t.Fatalf("run: %v", err)
	}
}

func TestRunRejectsUnknownScenario(t *testing.T) {
	if err := run([]string{"-scenario", "unknown"}); err == nil {
		t.Fatalf("expected error")
	}
}

package runtime

import (
	"testing"
	"time"
)

func TestEnvStringUsesFallbackWhenUnset(t *testing.T) {
	t.Setenv("SAFE_CONFIG_TEST_STRING", "")

	got := EnvString("SAFE_CONFIG_TEST_STRING", "fallback")
	if got != "fallback" {
		t.Fatalf("expected fallback, got %q", got)
	}
}

func TestEnvDurationParsesValue(t *testing.T) {
	t.Setenv("SAFE_CONFIG_TEST_DURATION", "250ms")

	got := EnvDuration("SAFE_CONFIG_TEST_DURATION", time.Second)
	if got != 250*time.Millisecond {
		t.Fatalf("expected 250ms, got %s", got)
	}
}

func TestEnvDurationUsesFallbackForInvalidValue(t *testing.T) {
	t.Setenv("SAFE_CONFIG_TEST_BAD_DURATION", "not-a-duration")

	got := EnvDuration("SAFE_CONFIG_TEST_BAD_DURATION", time.Second)
	if got != time.Second {
		t.Fatalf("expected fallback, got %s", got)
	}
}

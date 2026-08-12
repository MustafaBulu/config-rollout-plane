package domain

import (
	"errors"
	"testing"
)

func TestTenantValidateRequiresID(t *testing.T) {
	tenant := Tenant{Name: "payments"}

	err := tenant.Validate()
	if !errors.Is(err, ErrMissingID) {
		t.Fatalf("expected ErrMissingID, got %v", err)
	}
}

func TestConfigDefinitionValidateAcceptsJSONSchemaAndDefault(t *testing.T) {
	definition := ConfigDefinition{
		ID:           "cfg_01",
		TenantID:     "tenant_01",
		Key:          "payment.authorization.timeout",
		Schema:       []byte(`{"type":"integer","minimum":100,"maximum":10000}`),
		DefaultValue: []byte(`2000`),
	}

	if err := definition.Validate(); err != nil {
		t.Fatalf("expected valid definition, got %v", err)
	}
}

func TestConfigDefinitionValidateRejectsInvalidSchemaJSON(t *testing.T) {
	definition := ConfigDefinition{
		ID:       "cfg_01",
		TenantID: "tenant_01",
		Key:      "payment.authorization.timeout",
		Schema:   []byte(`{"type":"integer"`),
	}

	err := definition.Validate()
	if !errors.Is(err, ErrInvalidJSON) {
		t.Fatalf("expected ErrInvalidJSON, got %v", err)
	}
}

func TestConfigVersionValidateRequiresPositiveVersionNumber(t *testing.T) {
	version := ConfigVersion{
		ID:                 "ver_01",
		ConfigDefinitionID: "cfg_01",
		VersionNumber:      0,
		Value:              []byte(`1500`),
	}

	err := version.Validate()
	if !errors.Is(err, ErrInvalidVersionNumber) {
		t.Fatalf("expected ErrInvalidVersionNumber, got %v", err)
	}
}

func TestConfigVersionValidateRejectsInvalidValueJSON(t *testing.T) {
	version := ConfigVersion{
		ID:                 "ver_01",
		ConfigDefinitionID: "cfg_01",
		VersionNumber:      1,
		Value:              []byte(`{"timeout":`),
	}

	err := version.Validate()
	if !errors.Is(err, ErrInvalidJSON) {
		t.Fatalf("expected ErrInvalidJSON, got %v", err)
	}
}

func TestEnvironmentValidateRejectsUnknownEnvironment(t *testing.T) {
	err := Environment("prod").Validate()
	if !errors.Is(err, ErrInvalidEnvironment) {
		t.Fatalf("expected ErrInvalidEnvironment, got %v", err)
	}
}

func TestConfigEnvironmentStateWithStableVersionRejectsChangeDuringActiveRollout(t *testing.T) {
	state := ConfigEnvironmentState{
		ConfigDefinitionID: "cfg_01",
		Environment:        EnvironmentProduction,
		StableVersionID:    "ver_01",
		ActiveRolloutID:    "rollout_01",
	}

	_, err := state.WithStableVersion("ver_02")
	if !errors.Is(err, ErrStableVersionDuringRun) {
		t.Fatalf("expected ErrStableVersionDuringRun, got %v", err)
	}
}

func TestConfigEnvironmentStateWithStableVersionAllowsSameStableDuringActiveRollout(t *testing.T) {
	state := ConfigEnvironmentState{
		ConfigDefinitionID: "cfg_01",
		Environment:        EnvironmentProduction,
		StableVersionID:    "ver_01",
		ActiveRolloutID:    "rollout_01",
	}

	next, err := state.WithStableVersion("ver_01")
	if err != nil {
		t.Fatalf("expected stable version to remain unchanged, got %v", err)
	}
	if next.StableVersionID != "ver_01" {
		t.Fatalf("expected stable version ver_01, got %q", next.StableVersionID)
	}
}

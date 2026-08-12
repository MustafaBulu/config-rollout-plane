package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

var (
	ErrMissingID              = errors.New("id is required")
	ErrMissingTenantID        = errors.New("tenant id is required")
	ErrMissingConfigKey       = errors.New("config key is required")
	ErrMissingSchema          = errors.New("schema is required")
	ErrMissingValue           = errors.New("value is required")
	ErrInvalidJSON            = errors.New("json is invalid")
	ErrInvalidVersionNumber   = errors.New("version number must be positive")
	ErrInvalidEnvironment     = errors.New("environment is invalid")
	ErrStableVersionDuringRun = errors.New("stable version cannot change while rollout is active")
)

func (t Tenant) Validate() error {
	if strings.TrimSpace(t.ID) == "" {
		return ErrMissingID
	}
	if strings.TrimSpace(t.Name) == "" {
		return errors.New("tenant name is required")
	}
	return nil
}

func (d ConfigDefinition) Validate() error {
	if strings.TrimSpace(d.ID) == "" {
		return ErrMissingID
	}
	if strings.TrimSpace(d.TenantID) == "" {
		return ErrMissingTenantID
	}
	if strings.TrimSpace(d.Key) == "" {
		return ErrMissingConfigKey
	}
	if len(d.Schema) == 0 {
		return ErrMissingSchema
	}
	if !json.Valid(d.Schema) {
		return fmt.Errorf("%w: schema", ErrInvalidJSON)
	}
	if len(d.DefaultValue) > 0 && !json.Valid(d.DefaultValue) {
		return fmt.Errorf("%w: default value", ErrInvalidJSON)
	}
	return nil
}

func (v ConfigVersion) Validate() error {
	if strings.TrimSpace(v.ID) == "" {
		return ErrMissingID
	}
	if strings.TrimSpace(v.ConfigDefinitionID) == "" {
		return errors.New("config definition id is required")
	}
	if v.VersionNumber < 1 {
		return ErrInvalidVersionNumber
	}
	if len(v.Value) == 0 {
		return ErrMissingValue
	}
	if !json.Valid(v.Value) {
		return fmt.Errorf("%w: value", ErrInvalidJSON)
	}
	return nil
}

func (e Environment) Validate() error {
	switch e {
	case EnvironmentDevelopment, EnvironmentStaging, EnvironmentProduction:
		return nil
	default:
		return ErrInvalidEnvironment
	}
}

func (s ConfigEnvironmentState) Validate() error {
	if strings.TrimSpace(s.ConfigDefinitionID) == "" {
		return errors.New("config definition id is required")
	}
	if err := s.Environment.Validate(); err != nil {
		return err
	}
	return nil
}

func (s ConfigEnvironmentState) WithStableVersion(versionID string) (ConfigEnvironmentState, error) {
	if strings.TrimSpace(s.ActiveRolloutID) != "" && s.StableVersionID != versionID {
		return ConfigEnvironmentState{}, ErrStableVersionDuringRun
	}

	next := s
	next.StableVersionID = versionID
	return next, nil
}

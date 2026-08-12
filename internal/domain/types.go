package domain

import (
	"encoding/json"
	"time"
)

type Tenant struct {
	ID          string
	Name        string
	Description string
	Owner       string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type ConfigDefinition struct {
	ID           string
	TenantID     string
	Key          string
	Description  string
	Schema       json.RawMessage
	DefaultValue json.RawMessage
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type ConfigVersion struct {
	ID                 string
	ConfigDefinitionID string
	VersionNumber      int
	Value              json.RawMessage
	SourceCommitSHA    string
	CreatedBy          string
	CreatedAt          time.Time
}

type Environment string

const (
	EnvironmentDevelopment Environment = "development"
	EnvironmentStaging     Environment = "staging"
	EnvironmentProduction  Environment = "production"
)

type ConfigEnvironmentState struct {
	ConfigDefinitionID string
	Environment        Environment
	StableVersionID    string
	ActiveRolloutID    string
	UpdatedAt          time.Time
}

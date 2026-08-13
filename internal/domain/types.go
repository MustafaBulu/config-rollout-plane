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

type Agent struct {
	ID           string
	Service      string
	Environment  Environment
	Zone         string
	Instance     string
	Labels       map[string]string
	RegisteredAt time.Time
	LastSeenAt   time.Time
}

type AgentCredential struct {
	Token     string
	AgentID   string
	ExpiresAt time.Time
	CreatedAt time.Time
}

type AgentAcknowledgement struct {
	ID                 string
	AgentID            string
	ConfigDefinitionID string
	VersionID          string
	SnapshotRevision   int64
	Counted            bool
	CreatedAt          time.Time
}

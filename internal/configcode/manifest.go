package configcode

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"gopkg.in/yaml.v3"
)

const APIVersion = "safeconfig.dev/v1alpha1"

type Kind string

const (
	KindTenant           Kind = "Tenant"
	KindConfigDefinition Kind = "ConfigDefinition"
	KindConfigVersion    Kind = "ConfigVersion"
	KindStableVersion    Kind = "StableVersion"
	KindRollout          Kind = "Rollout"
)

type Manifest struct {
	APIVersion string          `yaml:"apiVersion"`
	Kind       Kind            `yaml:"kind"`
	Metadata   Metadata        `yaml:"metadata"`
	Spec       json.RawMessage `yaml:"-"`

	Tenant           TenantSpec           `yaml:"spec,omitempty"`
	ConfigDefinition ConfigDefinitionSpec `yaml:"-"`
	ConfigVersion    ConfigVersionSpec    `yaml:"-"`
	StableVersion    StableVersionSpec    `yaml:"-"`
	Rollout          RolloutSpec          `yaml:"-"`
}

type Metadata struct {
	Name string `yaml:"name"`
}

type TenantSpec struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description,omitempty"`
	Owner       string `yaml:"owner,omitempty"`
}

type ConfigDefinitionSpec struct {
	Tenant      string  `yaml:"tenant"`
	Key         string  `yaml:"key"`
	Description string  `yaml:"description,omitempty"`
	Schema      RawJSON `yaml:"schema"`
	Default     RawJSON `yaml:"default,omitempty"`
}

type ConfigVersionSpec struct {
	Tenant          string  `yaml:"tenant"`
	Key             string  `yaml:"key"`
	Value           RawJSON `yaml:"value"`
	SourceCommitSHA string  `yaml:"sourceCommitSHA,omitempty"`
	CreatedBy       string  `yaml:"createdBy,omitempty"`
}

type StableVersionSpec struct {
	Tenant        string `yaml:"tenant"`
	Key           string `yaml:"key"`
	Environment   string `yaml:"environment"`
	VersionNumber int    `yaml:"versionNumber"`
}

type RolloutSpec struct {
	Tenant                    string          `yaml:"tenant"`
	Key                       string          `yaml:"key"`
	Environment               string          `yaml:"environment"`
	CandidateVersion          int             `yaml:"candidateVersion"`
	TargetServices            []string        `yaml:"targetServices,omitempty"`
	Stages                    []RolloutStage  `yaml:"stages,omitempty"`
	Guardrails                []GuardrailRule `yaml:"guardrails,omitempty"`
	RequiredAckPercentage     float64         `yaml:"requiredAckPercentage,omitempty"`
	DeploymentTimeoutSeconds  int             `yaml:"deploymentTimeoutSeconds,omitempty"`
	RolloutMaxDurationSeconds int             `yaml:"rolloutMaxDurationSeconds,omitempty"`
	RollbackTimeoutSeconds    int             `yaml:"rollbackTimeoutSeconds,omitempty"`
}

type RolloutStage struct {
	ID                     string `yaml:"id"`
	Percentage             int    `yaml:"percentage"`
	MinimumDurationSeconds int    `yaml:"minimumDurationSeconds,omitempty"`
}

type GuardrailRule struct {
	Name                string  `yaml:"name"`
	Query               string  `yaml:"query"`
	Operator            string  `yaml:"operator"`
	Threshold           float64 `yaml:"threshold"`
	ConsecutiveFailures int     `yaml:"consecutiveFailures,omitempty"`
}

type RawJSON []byte

func (r RawJSON) MarshalJSON() ([]byte, error) {
	if len(r) == 0 {
		return []byte("null"), nil
	}
	return r, nil
}

func (r *RawJSON) UnmarshalYAML(value *yaml.Node) error {
	var decoded any
	if err := value.Decode(&decoded); err != nil {
		return err
	}
	normalized := normalizeYAML(decoded)
	data, err := json.Marshal(normalized)
	if err != nil {
		return err
	}
	*r = RawJSON(data)
	return nil
}

func (m *Manifest) UnmarshalYAML(value *yaml.Node) error {
	type header struct {
		APIVersion string   `yaml:"apiVersion"`
		Kind       Kind     `yaml:"kind"`
		Metadata   Metadata `yaml:"metadata"`
	}
	var h header
	if err := value.Decode(&h); err != nil {
		return err
	}

	var specNode *yaml.Node
	for i := 0; i+1 < len(value.Content); i += 2 {
		if value.Content[i].Value == "spec" {
			specNode = value.Content[i+1]
			break
		}
	}
	if specNode == nil {
		return fmt.Errorf("spec is required")
	}

	m.APIVersion = h.APIVersion
	m.Kind = h.Kind
	m.Metadata = h.Metadata

	switch h.Kind {
	case KindTenant:
		var spec TenantSpec
		if err := specNode.Decode(&spec); err != nil {
			return err
		}
		m.Tenant = spec
	case KindConfigDefinition:
		var spec ConfigDefinitionSpec
		if err := specNode.Decode(&spec); err != nil {
			return err
		}
		m.ConfigDefinition = spec
	case KindConfigVersion:
		var spec ConfigVersionSpec
		if err := specNode.Decode(&spec); err != nil {
			return err
		}
		m.ConfigVersion = spec
	case KindStableVersion:
		var spec StableVersionSpec
		if err := specNode.Decode(&spec); err != nil {
			return err
		}
		m.StableVersion = spec
	case KindRollout:
		var spec RolloutSpec
		if err := specNode.Decode(&spec); err != nil {
			return err
		}
		m.Rollout = spec
	default:
		var spec any
		if err := specNode.Decode(&spec); err != nil {
			return err
		}
	}

	rawSpec, err := yamlNodeToJSON(specNode)
	if err != nil {
		return err
	}
	m.Spec = rawSpec
	return nil
}

func DecodeManifests(r io.Reader) ([]Manifest, error) {
	decoder := yaml.NewDecoder(r)
	decoder.KnownFields(true)

	var manifests []Manifest
	for {
		var manifest Manifest
		if err := decoder.Decode(&manifest); err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		if manifest.APIVersion == "" && manifest.Kind == "" && manifest.Metadata.Name == "" && len(manifest.Spec) == 0 {
			continue
		}
		manifests = append(manifests, manifest)
	}
	return manifests, nil
}

func yamlNodeToJSON(node *yaml.Node) (json.RawMessage, error) {
	var decoded any
	if err := node.Decode(&decoded); err != nil {
		return nil, err
	}
	data, err := json.Marshal(normalizeYAML(decoded))
	if err != nil {
		return nil, err
	}
	return json.RawMessage(data), nil
}

func normalizeYAML(value any) any {
	switch typed := value.(type) {
	case map[any]any:
		result := make(map[string]any, len(typed))
		for key, val := range typed {
			result[fmt.Sprint(key)] = normalizeYAML(val)
		}
		return result
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, val := range typed {
			result[key] = normalizeYAML(val)
		}
		return result
	case []any:
		result := make([]any, 0, len(typed))
		for _, item := range typed {
			result = append(result, normalizeYAML(item))
		}
		return result
	default:
		return typed
	}
}

func (r RawJSON) Valid() bool {
	if len(r) == 0 {
		return false
	}
	return json.Valid(r)
}

func (r RawJSON) EqualJSON(other RawJSON) bool {
	return bytes.Equal(bytes.TrimSpace(r), bytes.TrimSpace(other))
}

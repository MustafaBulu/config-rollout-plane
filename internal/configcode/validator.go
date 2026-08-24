package configcode

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"config-rollout-plane/internal/configregistry"
	"config-rollout-plane/internal/domain"
	"config-rollout-plane/internal/guardrail"
	"config-rollout-plane/internal/rollout"
)

type Diagnostic struct {
	Path     string
	Document int
	Message  string
}

func (d Diagnostic) String() string {
	if d.Document > 0 {
		return fmt.Sprintf("%s document %d: %s", d.Path, d.Document, d.Message)
	}
	return fmt.Sprintf("%s: %s", d.Path, d.Message)
}

type Report struct {
	Files     int
	Manifests int
	Errors    []Diagnostic
}

func (r Report) OK() bool {
	return len(r.Errors) == 0
}

type Validator struct {
	SchemaValidator configregistry.SchemaValidator
}

func (v Validator) ValidatePaths(paths []string) Report {
	documents, files, diagnostics := LoadManifestDocuments(paths)
	report := Report{
		Files:  len(files),
		Errors: diagnostics,
	}
	if len(diagnostics) > 0 {
		return report
	}

	report.Manifests = len(documents)
	state := buildValidationState(documents)
	for _, document := range documents {
		diagnostics := v.validateManifest(document.Path, document.Document, document.Manifest, state)
		report.Errors = append(report.Errors, diagnostics...)
	}
	return report
}

func (v Validator) validateManifest(path string, document int, manifest Manifest, state validationState) []Diagnostic {
	var diagnostics []Diagnostic
	add := func(format string, args ...any) {
		diagnostics = append(diagnostics, Diagnostic{
			Path:     path,
			Document: document,
			Message:  fmt.Sprintf(format, args...),
		})
	}

	if manifest.APIVersion != APIVersion {
		add("apiVersion must be %q", APIVersion)
	}
	if strings.TrimSpace(manifest.Metadata.Name) == "" {
		add("metadata.name is required")
	}

	switch manifest.Kind {
	case KindTenant:
		if strings.TrimSpace(manifest.Metadata.Name) == "" {
			break
		}
		if strings.TrimSpace(manifest.Tenant.Name) == "" {
			add("spec.name is required")
		}
	case KindConfigDefinition:
		spec := manifest.ConfigDefinition
		if strings.TrimSpace(spec.Tenant) == "" {
			add("spec.tenant is required")
		}
		if strings.TrimSpace(spec.Key) == "" {
			add("spec.key is required")
		}
		if len(spec.Schema) == 0 {
			add("spec.schema is required")
			break
		}
		validator := v.schemaValidator()
		if err := validator.ValidateSchema(spec.Schema); err != nil {
			add("spec.schema is invalid: %v", err)
		}
		if len(spec.Default) > 0 {
			if !spec.Default.Valid() {
				add("spec.default must be valid JSON-compatible YAML")
			} else if err := validator.ValidateValue(spec.Schema, spec.Default); err != nil {
				add("spec.default does not match spec.schema: %v", err)
			}
		}
	case KindConfigVersion:
		spec := manifest.ConfigVersion
		if state.versionNameCounts[manifest.Metadata.Name] > 1 {
			add("ConfigVersion metadata.name %q must be unique", manifest.Metadata.Name)
		}
		if strings.TrimSpace(spec.Tenant) == "" {
			add("spec.tenant is required")
		}
		if strings.TrimSpace(spec.Key) == "" {
			add("spec.key is required")
		}
		if _, ok := state.definitions[configKey(spec.Tenant, spec.Key)]; !ok {
			add("matching ConfigDefinition for %s/%s is required", spec.Tenant, spec.Key)
		}
		if len(spec.Value) == 0 {
			add("spec.value is required")
			break
		}
		if !spec.Value.Valid() {
			add("spec.value must be valid JSON-compatible YAML")
		}
		key := configKey(spec.Tenant, spec.Key)
		if definition, ok := state.definitions[key]; ok && spec.Value.Valid() {
			if err := v.schemaValidator().ValidateValue(definition.Schema, spec.Value); err != nil {
				add("spec.value does not match matching ConfigDefinition schema: %v", err)
			}
		}
	case KindStableVersion:
		spec := manifest.StableVersion
		validateTenantKeyEnvironment(spec.Tenant, spec.Key, spec.Environment, add)
		if strings.TrimSpace(spec.VersionRef) == "" {
			add("spec.versionRef is required")
		} else if version, ok := state.versionRefs[spec.VersionRef]; !ok {
			add("spec.versionRef %q does not match a ConfigVersion metadata.name", spec.VersionRef)
		} else if version.Tenant != spec.Tenant || version.Key != spec.Key {
			add("spec.versionRef %q points to %s/%s, not %s/%s", spec.VersionRef, version.Tenant, version.Key, spec.Tenant, spec.Key)
		}
	case KindRollout:
		spec := manifest.Rollout
		validateTenantKeyEnvironment(spec.Tenant, spec.Key, spec.Environment, add)
		if strings.TrimSpace(spec.CandidateVersionRef) == "" {
			add("spec.candidateVersionRef is required")
		} else if version, ok := state.versionRefs[spec.CandidateVersionRef]; !ok {
			add("spec.candidateVersionRef %q does not match a ConfigVersion metadata.name", spec.CandidateVersionRef)
		} else if version.Tenant != spec.Tenant || version.Key != spec.Key {
			add("spec.candidateVersionRef %q points to %s/%s, not %s/%s", spec.CandidateVersionRef, version.Tenant, version.Key, spec.Tenant, spec.Key)
		}
		if len(spec.Stages) > 0 {
			stages := make([]rollout.Stage, 0, len(spec.Stages))
			for _, stage := range spec.Stages {
				if stage.MinimumDurationSeconds < 0 {
					add("stage %q minimumDurationSeconds cannot be negative", stage.ID)
				}
				stages = append(stages, rollout.Stage{ID: stage.ID, Percentage: stage.Percentage})
			}
			if err := rollout.ValidateStagePlan(stages); err != nil {
				add("spec.stages is invalid: %v", err)
			}
		}
		for _, rule := range spec.Guardrails {
			if strings.TrimSpace(rule.Query) == "" {
				add("guardrail %q query is required", rule.Name)
			}
			guardrailRule := guardrail.Rule{
				Name:                rule.Name,
				Query:               rule.Query,
				Operator:            guardrail.Operator(rule.Operator),
				Threshold:           rule.Threshold,
				ConsecutiveFailures: rule.ConsecutiveFailures,
			}
			if err := guardrailRule.Validate(); err != nil {
				add("guardrail %q is invalid: %v", rule.Name, err)
			}
		}
		if spec.RequiredAckPercentage < 0 || spec.RequiredAckPercentage > 100 {
			add("spec.requiredAckPercentage must be between 0 and 100")
		}
		if spec.DeploymentTimeoutSeconds < 0 {
			add("spec.deploymentTimeoutSeconds cannot be negative")
		}
		if spec.RolloutMaxDurationSeconds < 0 {
			add("spec.rolloutMaxDurationSeconds cannot be negative")
		}
		if spec.RollbackTimeoutSeconds < 0 {
			add("spec.rollbackTimeoutSeconds cannot be negative")
		}
	default:
		add("kind must be one of Tenant, ConfigDefinition, ConfigVersion, StableVersion, Rollout")
	}

	return diagnostics
}

func (v Validator) schemaValidator() configregistry.SchemaValidator {
	if v.SchemaValidator != nil {
		return v.SchemaValidator
	}
	return configregistry.JSONSchemaValidator{}
}

type validationState struct {
	definitions       map[string]ConfigDefinitionSpec
	versionRefs       map[string]ConfigVersionSpec
	versionNameCounts map[string]int
}

type ManifestDocument struct {
	Path     string
	Document int
	Manifest Manifest
}

func buildValidationState(documents []ManifestDocument) validationState {
	state := validationState{
		definitions:       make(map[string]ConfigDefinitionSpec),
		versionRefs:       make(map[string]ConfigVersionSpec),
		versionNameCounts: make(map[string]int),
	}
	for _, document := range documents {
		switch document.Manifest.Kind {
		case KindConfigDefinition:
			spec := document.Manifest.ConfigDefinition
			if strings.TrimSpace(spec.Tenant) != "" && strings.TrimSpace(spec.Key) != "" && len(spec.Schema) > 0 {
				state.definitions[configKey(spec.Tenant, spec.Key)] = spec
			}
		case KindConfigVersion:
			spec := document.Manifest.ConfigVersion
			state.versionNameCounts[document.Manifest.Metadata.Name]++
			if strings.TrimSpace(spec.Tenant) != "" && strings.TrimSpace(spec.Key) != "" {
				state.versionRefs[document.Manifest.Metadata.Name] = spec
			}
		}
	}
	return state
}

func validateTenantKeyEnvironment(tenant string, key string, environment string, add func(string, ...any)) {
	if strings.TrimSpace(tenant) == "" {
		add("spec.tenant is required")
	}
	if strings.TrimSpace(key) == "" {
		add("spec.key is required")
	}
	if err := domain.Environment(environment).Validate(); err != nil {
		add("spec.environment is invalid: %v", err)
	}
}

func LoadManifestDocuments(paths []string) ([]ManifestDocument, []string, []Diagnostic) {
	files, diagnostics := collectManifestFiles(paths)
	if len(diagnostics) > 0 {
		return nil, files, diagnostics
	}

	var documents []ManifestDocument
	for _, file := range files {
		manifests, err := readManifestFile(file)
		if err != nil {
			diagnostics = append(diagnostics, Diagnostic{Path: file, Message: err.Error()})
			continue
		}
		for i, manifest := range manifests {
			documents = append(documents, ManifestDocument{
				Path:     file,
				Document: i + 1,
				Manifest: manifest,
			})
		}
	}
	return documents, files, diagnostics
}

func collectManifestFiles(paths []string) ([]string, []Diagnostic) {
	if len(paths) == 0 {
		paths = []string{"config"}
	}

	var files []string
	var diagnostics []Diagnostic
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			diagnostics = append(diagnostics, Diagnostic{Path: path, Message: err.Error()})
			continue
		}
		if !info.IsDir() {
			if isYAML(path) {
				files = append(files, path)
			}
			continue
		}
		err = filepath.WalkDir(path, func(file string, entry os.DirEntry, err error) error {
			if err != nil {
				diagnostics = append(diagnostics, Diagnostic{Path: file, Message: err.Error()})
				return nil
			}
			if entry.IsDir() {
				return nil
			}
			if isYAML(file) {
				files = append(files, file)
			}
			return nil
		})
		if err != nil {
			diagnostics = append(diagnostics, Diagnostic{Path: path, Message: err.Error()})
		}
	}
	sort.Strings(files)
	if len(files) == 0 && len(diagnostics) == 0 {
		diagnostics = append(diagnostics, Diagnostic{Path: strings.Join(paths, ","), Message: "no YAML manifest files found"})
	}
	return files, diagnostics
}

func readManifestFile(path string) ([]Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, nil
	}
	return DecodeManifests(bytes.NewReader(data))
}

func isYAML(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".yaml" || ext == ".yml"
}

func configKey(tenant string, key string) string {
	return tenant + "/" + key
}

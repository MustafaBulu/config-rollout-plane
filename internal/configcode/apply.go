package configcode

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type ApplyOptions struct {
	BaseURL         string
	Token           string
	DryRun          bool
	IncludeRollouts bool
	HTTPClient      *http.Client
}

type ApplyReport struct {
	Steps []ApplyStep
}

type ApplyStep struct {
	Kind    Kind
	Name    string
	Action  string
	Status  string
	Message string
}

type Applier struct {
	Options ApplyOptions
}

type applyState struct {
	versionRefs map[string]versionResponse
}

func (a Applier) ApplyPaths(ctx context.Context, paths []string) (ApplyReport, error) {
	documents, _, diagnostics := LoadManifestDocuments(paths)
	if len(diagnostics) > 0 {
		return ApplyReport{}, diagnosticsError(diagnostics)
	}
	return a.ApplyDocuments(ctx, documents)
}

func (a Applier) ApplyDocuments(ctx context.Context, documents []ManifestDocument) (ApplyReport, error) {
	if strings.TrimSpace(a.Options.BaseURL) == "" && !a.Options.DryRun {
		return ApplyReport{}, fmt.Errorf("control-plane URL is required")
	}

	ordered := orderDocuments(documents)
	state := applyState{versionRefs: make(map[string]versionResponse)}
	report := ApplyReport{Steps: make([]ApplyStep, 0, len(ordered))}
	for _, document := range ordered {
		step, err := a.applyDocument(ctx, document.Manifest, &state)
		report.Steps = append(report.Steps, step)
		if err != nil {
			return report, err
		}
	}
	return report, nil
}

func (a Applier) applyDocument(ctx context.Context, manifest Manifest, state *applyState) (ApplyStep, error) {
	if a.Options.DryRun {
		return ApplyStep{
			Kind:    manifest.Kind,
			Name:    manifest.Metadata.Name,
			Action:  plannedAction(manifest.Kind),
			Status:  "planned",
			Message: "dry run",
		}, nil
	}

	switch manifest.Kind {
	case KindTenant:
		return a.applyTenant(ctx, manifest)
	case KindConfigDefinition:
		return a.applyDefinition(ctx, manifest)
	case KindConfigVersion:
		return a.applyVersion(ctx, manifest, state)
	case KindStableVersion:
		return a.applyStableVersion(ctx, manifest, state)
	case KindRollout:
		if !a.Options.IncludeRollouts {
			return ApplyStep{
				Kind:    manifest.Kind,
				Name:    manifest.Metadata.Name,
				Action:  "start",
				Status:  "skipped",
				Message: "rollout requires --include-rollouts",
			}, nil
		}
		return a.applyRollout(ctx, manifest, state)
	default:
		return ApplyStep{}, fmt.Errorf("unsupported manifest kind %q", manifest.Kind)
	}
}

func (a Applier) applyTenant(ctx context.Context, manifest Manifest) (ApplyStep, error) {
	name := manifest.Metadata.Name
	step := ApplyStep{Kind: manifest.Kind, Name: name, Action: "create"}
	var existing tenantResponse
	status, err := a.get(ctx, "/v1/tenants/"+escape(name), &existing)
	if err != nil {
		return step, err
	}
	if status == http.StatusOK {
		step.Status = "skipped"
		step.Message = "tenant already exists"
		return step, nil
	}
	if status != http.StatusNotFound {
		return step, fmt.Errorf("get tenant %q returned status %d", name, status)
	}

	payload := createTenantRequest{
		ID:          name,
		Name:        manifest.Tenant.Name,
		Description: manifest.Tenant.Description,
		Owner:       manifest.Tenant.Owner,
	}
	if err := a.post(ctx, "/v1/tenants", payload, http.StatusCreated, nil); err != nil {
		return step, err
	}
	step.Status = "applied"
	step.Message = "tenant created"
	return step, nil
}

func (a Applier) applyDefinition(ctx context.Context, manifest Manifest) (ApplyStep, error) {
	spec := manifest.ConfigDefinition
	step := ApplyStep{Kind: manifest.Kind, Name: manifest.Metadata.Name, Action: "create"}
	var existing definitionResponse
	status, err := a.get(ctx, "/v1/tenants/"+escape(spec.Tenant)+"/configs/"+escape(spec.Key), &existing)
	if err != nil {
		return step, err
	}
	if status == http.StatusOK {
		if !RawJSON(existing.Schema).EqualJSON(spec.Schema) {
			return step, fmt.Errorf("config definition %s/%s already exists with different schema", spec.Tenant, spec.Key)
		}
		if len(spec.Default) > 0 && !RawJSON(existing.DefaultValue).EqualJSON(spec.Default) {
			return step, fmt.Errorf("config definition %s/%s already exists with different default", spec.Tenant, spec.Key)
		}
		step.Status = "skipped"
		step.Message = "config definition already exists"
		return step, nil
	}
	if status != http.StatusNotFound {
		return step, fmt.Errorf("get config definition %s/%s returned status %d", spec.Tenant, spec.Key, status)
	}

	payload := createDefinitionRequest{
		Key:         spec.Key,
		Description: spec.Description,
		Schema:      spec.Schema,
		Default:     spec.Default,
	}
	if err := a.post(ctx, "/v1/tenants/"+escape(spec.Tenant)+"/configs", payload, http.StatusCreated, nil); err != nil {
		return step, err
	}
	step.Status = "applied"
	step.Message = "config definition created"
	return step, nil
}

func (a Applier) applyVersion(ctx context.Context, manifest Manifest, state *applyState) (ApplyStep, error) {
	spec := manifest.ConfigVersion
	step := ApplyStep{Kind: manifest.Kind, Name: manifest.Metadata.Name, Action: "create"}
	versions, err := a.listVersions(ctx, spec.Tenant, spec.Key)
	if err != nil {
		return step, err
	}
	for _, version := range versions {
		if RawJSON(version.Value).EqualJSON(spec.Value) {
			state.versionRefs[manifest.Metadata.Name] = version
			step.Status = "skipped"
			step.Message = fmt.Sprintf("matching config version already exists as version %d", version.VersionNumber)
			return step, nil
		}
	}

	payload := createVersionRequest{
		Value:           spec.Value,
		SourceCommitSHA: spec.SourceCommitSHA,
		CreatedBy:       spec.CreatedBy,
	}
	var created versionResponse
	if err := a.post(ctx, "/v1/tenants/"+escape(spec.Tenant)+"/configs/"+escape(spec.Key)+"/versions", payload, http.StatusCreated, &created); err != nil {
		return step, err
	}
	state.versionRefs[manifest.Metadata.Name] = created
	step.Status = "applied"
	step.Message = fmt.Sprintf("config version %d created", created.VersionNumber)
	return step, nil
}

func (a Applier) applyStableVersion(ctx context.Context, manifest Manifest, state *applyState) (ApplyStep, error) {
	spec := manifest.StableVersion
	step := ApplyStep{Kind: manifest.Kind, Name: manifest.Metadata.Name, Action: "set"}

	desiredVersion, ok := state.versionRefs[spec.VersionRef]
	if !ok {
		return step, fmt.Errorf("versionRef %q was not applied", spec.VersionRef)
	}

	var envState environmentStateResponse
	status, err := a.get(ctx, stablePath(spec.Tenant, spec.Key, spec.Environment), &envState)
	if err != nil {
		return step, err
	}
	if status == http.StatusOK && envState.StableVersionID == desiredVersion.ID {
		step.Status = "skipped"
		step.Message = "stable version already set"
		return step, nil
	}
	if status != http.StatusOK && status != http.StatusNotFound {
		return step, fmt.Errorf("get stable version %s/%s/%s returned status %d", spec.Tenant, spec.Key, spec.Environment, status)
	}

	payload := setStableVersionRequest{VersionNumber: desiredVersion.VersionNumber}
	if err := a.post(ctx, stablePath(spec.Tenant, spec.Key, spec.Environment), payload, http.StatusOK, nil); err != nil {
		return step, err
	}
	step.Status = "applied"
	step.Message = "stable version set"
	return step, nil
}

func (a Applier) applyRollout(ctx context.Context, manifest Manifest, state *applyState) (ApplyStep, error) {
	spec := manifest.Rollout
	step := ApplyStep{Kind: manifest.Kind, Name: manifest.Metadata.Name, Action: "start"}
	candidateVersion, ok := state.versionRefs[spec.CandidateVersionRef]
	if !ok {
		return step, fmt.Errorf("candidateVersionRef %q was not applied", spec.CandidateVersionRef)
	}
	payload := createRolloutRequest{
		TenantID:                  spec.Tenant,
		Key:                       spec.Key,
		Environment:               spec.Environment,
		CandidateVersion:          candidateVersion.VersionNumber,
		TargetServices:            spec.TargetServices,
		Stages:                    spec.Stages,
		Guardrails:                spec.Guardrails,
		RequiredAckPercentage:     spec.RequiredAckPercentage,
		DeploymentTimeoutSeconds:  spec.DeploymentTimeoutSeconds,
		RolloutMaxDurationSeconds: spec.RolloutMaxDurationSeconds,
		RollbackTimeoutSeconds:    spec.RollbackTimeoutSeconds,
	}
	if err := a.post(ctx, "/v1/rollouts", payload, http.StatusCreated, nil); err != nil {
		return step, err
	}
	step.Status = "applied"
	step.Message = "rollout started"
	return step, nil
}

func (a Applier) listVersions(ctx context.Context, tenant string, key string) ([]versionResponse, error) {
	var versions []versionResponse
	status, err := a.get(ctx, "/v1/tenants/"+escape(tenant)+"/configs/"+escape(key)+"/versions", &versions)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("list config versions %s/%s returned status %d", tenant, key, status)
	}
	return versions, nil
}

func (a Applier) get(ctx context.Context, path string, dst any) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.url(path), nil)
	if err != nil {
		return 0, err
	}
	a.authorize(req)
	resp, err := a.client().Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
			return 0, err
		}
		return resp.StatusCode, nil
	}
	if _, err := io.Copy(io.Discard, io.LimitReader(resp.Body, 1024)); err != nil {
		return 0, err
	}
	return resp.StatusCode, nil
}

func (a Applier) post(ctx context.Context, path string, payload any, expectedStatus int, dst any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.url(path), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	a.authorize(req)

	resp, err := a.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != expectedStatus {
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 2048))
		if readErr != nil {
			return readErr
		}
		return fmt.Errorf("POST %s returned status %d: %s", path, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if dst != nil {
		if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
			return err
		}
	} else {
		if _, err := io.Copy(io.Discard, resp.Body); err != nil {
			return err
		}
	}
	return nil
}

func (a Applier) client() *http.Client {
	if a.Options.HTTPClient != nil {
		return a.Options.HTTPClient
	}
	return &http.Client{Timeout: 10 * time.Second}
}

func (a Applier) url(path string) string {
	return strings.TrimRight(a.Options.BaseURL, "/") + path
}

func (a Applier) authorize(req *http.Request) {
	if strings.TrimSpace(a.Options.Token) != "" {
		req.Header.Set("Authorization", "Bearer "+a.Options.Token)
	}
}

func orderDocuments(documents []ManifestDocument) []ManifestDocument {
	ordered := make([]ManifestDocument, 0, len(documents))
	for _, kind := range []Kind{KindTenant, KindConfigDefinition, KindConfigVersion, KindStableVersion, KindRollout} {
		for _, document := range documents {
			if document.Manifest.Kind == kind {
				ordered = append(ordered, document)
			}
		}
	}
	return ordered
}

func plannedAction(kind Kind) string {
	switch kind {
	case KindStableVersion:
		return "set"
	case KindRollout:
		return "start"
	default:
		return "create"
	}
}

func stablePath(tenant string, key string, environment string) string {
	return "/v1/tenants/" + escape(tenant) + "/configs/" + escape(key) + "/environments/" + escape(environment) + "/stable"
}

func escape(value string) string {
	return url.PathEscape(value)
}

type diagnosticsError []Diagnostic

func (e diagnosticsError) Error() string {
	messages := make([]string, 0, len(e))
	for _, diagnostic := range e {
		messages = append(messages, diagnostic.String())
	}
	return strings.Join(messages, "\n")
}

type createTenantRequest struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Owner       string `json:"owner,omitempty"`
}

type createDefinitionRequest struct {
	Key         string  `json:"key"`
	Description string  `json:"description,omitempty"`
	Schema      RawJSON `json:"schema"`
	Default     RawJSON `json:"default,omitempty"`
}

type createVersionRequest struct {
	Value           RawJSON `json:"value"`
	SourceCommitSHA string  `json:"source_commit_sha,omitempty"`
	CreatedBy       string  `json:"created_by,omitempty"`
}

type setStableVersionRequest struct {
	VersionNumber int `json:"version_number"`
}

type createRolloutRequest struct {
	TenantID                  string          `json:"tenant_id"`
	Key                       string          `json:"key"`
	Environment               string          `json:"environment"`
	CandidateVersion          int             `json:"candidate_version"`
	TargetServices            []string        `json:"target_services,omitempty"`
	Stages                    []RolloutStage  `json:"stages,omitempty"`
	Guardrails                []GuardrailRule `json:"guardrails,omitempty"`
	RequiredAckPercentage     float64         `json:"required_ack_percentage,omitempty"`
	DeploymentTimeoutSeconds  int             `json:"deployment_timeout_seconds,omitempty"`
	RolloutMaxDurationSeconds int             `json:"rollout_max_duration_seconds,omitempty"`
	RollbackTimeoutSeconds    int             `json:"rollback_timeout_seconds,omitempty"`
}

type tenantResponse struct {
	ID string `json:"id"`
}

type definitionResponse struct {
	Schema       RawJSON `json:"schema"`
	DefaultValue RawJSON `json:"default,omitempty"`
}

type versionResponse struct {
	ID            string  `json:"id"`
	VersionNumber int     `json:"version_number"`
	Value         RawJSON `json:"value"`
}

type environmentStateResponse struct {
	StableVersionID string `json:"stable_version_id"`
}

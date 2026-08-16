package controlplane

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"config-rollout-plane/internal/agentregistry"
	"config-rollout-plane/internal/configregistry"
	"config-rollout-plane/internal/domain"
	"config-rollout-plane/internal/health"
	"config-rollout-plane/internal/rollout"
)

type Handler struct {
	registry *configregistry.Service
	agents   *agentregistry.Service
	rollouts *rollout.Service
}

func NewHandler(registry *configregistry.Service) http.Handler {
	return NewHandlerWithReadiness(registry, nil, health.StaticChecker{})
}

func NewHandlerWithReadiness(registry *configregistry.Service, agents *agentregistry.Service, ready health.Checker) http.Handler {
	return NewHandlerWithServices(registry, agents, nil, ready)
}

func NewHandlerWithServices(registry *configregistry.Service, agents *agentregistry.Service, rollouts *rollout.Service, ready health.Checker) http.Handler {
	h := Handler{registry: registry, agents: agents, rollouts: rollouts}

	mux := http.NewServeMux()
	healthHandler := health.NewHandler("control-plane", ready)
	mux.Handle("/healthz", healthHandler)
	mux.Handle("/livez", healthHandler)
	mux.Handle("/readyz", healthHandler)

	mux.HandleFunc("POST /v1/tenants", h.createTenant)
	mux.HandleFunc("GET /v1/tenants", h.listTenants)
	mux.HandleFunc("GET /v1/tenants/{tenant}", h.getTenant)

	mux.HandleFunc("POST /v1/tenants/{tenant}/configs", h.createDefinition)
	mux.HandleFunc("GET /v1/tenants/{tenant}/configs", h.listDefinitions)
	mux.HandleFunc("GET /v1/tenants/{tenant}/configs/{key}", h.getDefinition)

	mux.HandleFunc("POST /v1/tenants/{tenant}/configs/{key}/versions", h.createVersion)
	mux.HandleFunc("GET /v1/tenants/{tenant}/configs/{key}/versions", h.listVersions)

	mux.HandleFunc("POST /v1/tenants/{tenant}/configs/{key}/environments/{environment}/stable", h.setStableVersion)
	mux.HandleFunc("GET /v1/tenants/{tenant}/configs/{key}/environments/{environment}/stable", h.getStableVersion)

	mux.HandleFunc("POST /v1/agents/register", h.registerAgent)
	mux.HandleFunc("POST /v1/agents/{agentID}/heartbeat", h.agentHeartbeat)
	mux.HandleFunc("POST /v1/agents/{agentID}/acknowledgements", h.agentAcknowledgement)

	mux.HandleFunc("POST /v1/rollouts", h.createRollout)
	mux.HandleFunc("GET /v1/rollouts/{rolloutID}", h.getRollout)

	return mux
}

func (h Handler) createTenant(w http.ResponseWriter, r *http.Request) {
	var req createTenantRequest
	if !decodeRequest(w, r, &req) {
		return
	}

	tenant, err := h.registry.CreateTenant(r.Context(), configregistry.CreateTenantInput{
		ID:          req.ID,
		Name:        req.Name,
		Description: req.Description,
		Owner:       req.Owner,
	})
	if err != nil {
		writeError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, tenantResponseFromDomain(tenant))
}

func (h Handler) listTenants(w http.ResponseWriter, r *http.Request) {
	tenants, err := h.registry.ListTenants(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}

	response := make([]tenantResponse, 0, len(tenants))
	for _, tenant := range tenants {
		response = append(response, tenantResponseFromDomain(tenant))
	}
	writeJSON(w, http.StatusOK, response)
}

func (h Handler) getTenant(w http.ResponseWriter, r *http.Request) {
	tenant, err := h.registry.GetTenant(r.Context(), r.PathValue("tenant"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, tenantResponseFromDomain(tenant))
}

func (h Handler) createDefinition(w http.ResponseWriter, r *http.Request) {
	var req createDefinitionRequest
	if !decodeRequest(w, r, &req) {
		return
	}

	definition, err := h.registry.CreateDefinition(r.Context(), configregistry.CreateDefinitionInput{
		TenantID:     r.PathValue("tenant"),
		Key:          req.Key,
		Description:  req.Description,
		Schema:       req.Schema,
		DefaultValue: req.Default,
	})
	if err != nil {
		writeError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, definitionResponseFromDomain(definition))
}

func (h Handler) listDefinitions(w http.ResponseWriter, r *http.Request) {
	definitions, err := h.registry.ListDefinitions(r.Context(), r.PathValue("tenant"))
	if err != nil {
		writeError(w, err)
		return
	}

	response := make([]definitionResponse, 0, len(definitions))
	for _, definition := range definitions {
		response = append(response, definitionResponseFromDomain(definition))
	}
	writeJSON(w, http.StatusOK, response)
}

func (h Handler) getDefinition(w http.ResponseWriter, r *http.Request) {
	definition, err := h.registry.GetDefinition(r.Context(), r.PathValue("tenant"), r.PathValue("key"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, definitionResponseFromDomain(definition))
}

func (h Handler) createVersion(w http.ResponseWriter, r *http.Request) {
	var req createVersionRequest
	if !decodeRequest(w, r, &req) {
		return
	}

	version, err := h.registry.CreateVersion(r.Context(), configregistry.CreateVersionInput{
		TenantID:        r.PathValue("tenant"),
		Key:             r.PathValue("key"),
		Value:           req.Value,
		SourceCommitSHA: req.SourceCommitSHA,
		CreatedBy:       req.CreatedBy,
	})
	if err != nil {
		writeError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, versionResponseFromDomain(version))
}

func (h Handler) listVersions(w http.ResponseWriter, r *http.Request) {
	versions, err := h.registry.ListVersions(r.Context(), r.PathValue("tenant"), r.PathValue("key"))
	if err != nil {
		writeError(w, err)
		return
	}

	response := make([]versionResponse, 0, len(versions))
	for _, version := range versions {
		response = append(response, versionResponseFromDomain(version))
	}
	writeJSON(w, http.StatusOK, response)
}

func (h Handler) setStableVersion(w http.ResponseWriter, r *http.Request) {
	var req setStableVersionRequest
	if !decodeRequest(w, r, &req) {
		return
	}

	state, err := h.registry.SetStableVersion(r.Context(), configregistry.SetStableVersionInput{
		TenantID:      r.PathValue("tenant"),
		Key:           r.PathValue("key"),
		Environment:   domain.Environment(r.PathValue("environment")),
		VersionNumber: req.VersionNumber,
	})
	if err != nil {
		writeError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, environmentStateResponseFromDomain(state))
}

func (h Handler) getStableVersion(w http.ResponseWriter, r *http.Request) {
	state, err := h.registry.GetEnvironmentState(
		r.Context(),
		r.PathValue("tenant"),
		r.PathValue("key"),
		domain.Environment(r.PathValue("environment")),
	)
	if err != nil {
		writeError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, environmentStateResponseFromDomain(state))
}

func (h Handler) registerAgent(w http.ResponseWriter, r *http.Request) {
	if h.agents == nil {
		writeJSON(w, http.StatusServiceUnavailable, errorResponse{Error: "unavailable", Message: "agent registry is unavailable"})
		return
	}

	var req registerAgentRequest
	if !decodeRequest(w, r, &req) {
		return
	}

	result, err := h.agents.Register(r.Context(), agentregistry.RegisterInput{
		BootstrapToken: req.BootstrapToken,
		ID:             req.ID,
		Service:        req.Service,
		Environment:    domain.Environment(req.Environment),
		Zone:           req.Zone,
		Instance:       req.Instance,
		Labels:         req.Labels,
	})
	if err != nil {
		writeAgentError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, registerAgentResponseFromDomain(result))
}

func (h Handler) agentHeartbeat(w http.ResponseWriter, r *http.Request) {
	if h.agents == nil {
		writeJSON(w, http.StatusServiceUnavailable, errorResponse{Error: "unavailable", Message: "agent registry is unavailable"})
		return
	}

	agent, err := h.agents.Heartbeat(r.Context(), r.PathValue("agentID"), bearerToken(r.Header.Get("Authorization")))
	if err != nil {
		writeAgentError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, agentResponseFromDomain(agent))
}

func (h Handler) agentAcknowledgement(w http.ResponseWriter, r *http.Request) {
	if h.agents == nil {
		writeJSON(w, http.StatusServiceUnavailable, errorResponse{Error: "unavailable", Message: "agent registry is unavailable"})
		return
	}

	var req acknowledgementRequest
	if !decodeRequest(w, r, &req) {
		return
	}

	ack, err := h.agents.Acknowledge(r.Context(), agentregistry.AcknowledgeInput{
		AgentID:            r.PathValue("agentID"),
		Token:              bearerToken(r.Header.Get("Authorization")),
		ConfigDefinitionID: req.ConfigDefinitionID,
		VersionID:          req.VersionID,
		SnapshotRevision:   req.SnapshotRevision,
	})
	if err != nil {
		writeAgentError(w, err)
		return
	}

	var rolloutResult *rolloutAckResponse
	if h.rollouts != nil && req.RolloutID != "" && req.StageID != "" {
		result, err := h.rollouts.Acknowledge(r.Context(), rollout.AcknowledgeInput{
			RolloutID:        req.RolloutID,
			StageID:          req.StageID,
			AgentID:          r.PathValue("agentID"),
			VersionID:        req.VersionID,
			SnapshotRevision: req.SnapshotRevision,
		})
		if err != nil {
			writeRolloutError(w, err)
			return
		}
		rolloutResult = &rolloutAckResponse{
			Counted:  result.Counted,
			Decision: string(result.Decision),
			Coverage: coverageResponseFromDomain(result.Coverage),
		}
	}

	response := acknowledgementResponseFromDomain(ack)
	response.Rollout = rolloutResult
	writeJSON(w, http.StatusCreated, response)
}

func (h Handler) createRollout(w http.ResponseWriter, r *http.Request) {
	if h.rollouts == nil {
		writeJSON(w, http.StatusServiceUnavailable, errorResponse{Error: "unavailable", Message: "rollout service is unavailable"})
		return
	}

	var req createRolloutRequest
	if !decodeRequest(w, r, &req) {
		return
	}

	created, targets, err := h.rollouts.CreateRollout(r.Context(), rollout.CreateRolloutInput{
		TenantID:               req.TenantID,
		Key:                    req.Key,
		Environment:            domain.Environment(req.Environment),
		CandidateVersionNumber: req.CandidateVersion,
		TargetServices:         req.TargetServices,
		Stages:                 stagesFromRequest(req.Stages),
		RequiredAckPercentage:  req.RequiredAckPercentage,
		DeploymentTimeout:      durationSeconds(req.DeploymentTimeoutSeconds),
	})
	if err != nil {
		writeRolloutError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, rolloutResponseFromDomain(created, targets, rollout.AcknowledgementCoverage(targets)))
}

func (h Handler) getRollout(w http.ResponseWriter, r *http.Request) {
	if h.rollouts == nil {
		writeJSON(w, http.StatusServiceUnavailable, errorResponse{Error: "unavailable", Message: "rollout service is unavailable"})
		return
	}

	got, targets, coverage, err := h.rollouts.GetRollout(r.Context(), r.PathValue("rolloutID"))
	if err != nil {
		writeRolloutError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, rolloutResponseFromDomain(got, targets, coverage))
}

func decodeRequest(w http.ResponseWriter, r *http.Request, dst any) bool {
	defer r.Body.Close()

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid_json", Message: err.Error()})
		return false
	}
	return true
}

func writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, configregistry.ErrInvalidInput):
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid_input", Message: err.Error()})
	case errors.Is(err, configregistry.ErrNotFound):
		writeJSON(w, http.StatusNotFound, errorResponse{Error: "not_found", Message: err.Error()})
	case errors.Is(err, configregistry.ErrAlreadyExists):
		writeJSON(w, http.StatusConflict, errorResponse{Error: "already_exists", Message: err.Error()})
	case errors.Is(err, configregistry.ErrConflict):
		writeJSON(w, http.StatusConflict, errorResponse{Error: "conflict", Message: err.Error()})
	default:
		writeJSON(w, http.StatusInternalServerError, errorResponse{Error: "internal_error", Message: "request failed"})
	}
}

func writeAgentError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, agentregistry.ErrInvalidInput):
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid_input", Message: err.Error()})
	case errors.Is(err, agentregistry.ErrUnauthorized), errors.Is(err, agentregistry.ErrExpiredCredential):
		writeJSON(w, http.StatusUnauthorized, errorResponse{Error: "unauthorized", Message: err.Error()})
	case errors.Is(err, agentregistry.ErrForbidden):
		writeJSON(w, http.StatusForbidden, errorResponse{Error: "forbidden", Message: err.Error()})
	case errors.Is(err, agentregistry.ErrNotFound):
		writeJSON(w, http.StatusNotFound, errorResponse{Error: "not_found", Message: err.Error()})
	default:
		writeJSON(w, http.StatusInternalServerError, errorResponse{Error: "internal_error", Message: "request failed"})
	}
}

func writeRolloutError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, rollout.ErrInvalidInput):
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid_input", Message: err.Error()})
	case errors.Is(err, rollout.ErrNotFound):
		writeJSON(w, http.StatusNotFound, errorResponse{Error: "not_found", Message: err.Error()})
	case errors.Is(err, rollout.ErrAlreadyExists), errors.Is(err, rollout.ErrConflict):
		writeJSON(w, http.StatusConflict, errorResponse{Error: "conflict", Message: err.Error()})
	default:
		writeJSON(w, http.StatusInternalServerError, errorResponse{Error: "internal_error", Message: "request failed"})
	}
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

type createTenantRequest struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Owner       string `json:"owner"`
}

type createDefinitionRequest struct {
	Key         string          `json:"key"`
	Description string          `json:"description"`
	Schema      json.RawMessage `json:"schema"`
	Default     json.RawMessage `json:"default"`
}

type createVersionRequest struct {
	Value           json.RawMessage `json:"value"`
	SourceCommitSHA string          `json:"source_commit_sha"`
	CreatedBy       string          `json:"created_by"`
}

type setStableVersionRequest struct {
	VersionNumber int `json:"version_number"`
}

type registerAgentRequest struct {
	BootstrapToken string            `json:"bootstrap_token"`
	ID             string            `json:"id"`
	Service        string            `json:"service"`
	Environment    string            `json:"environment"`
	Zone           string            `json:"zone"`
	Instance       string            `json:"instance"`
	Labels         map[string]string `json:"labels"`
}

type acknowledgementRequest struct {
	ConfigDefinitionID string `json:"config_definition_id"`
	VersionID          string `json:"version_id"`
	SnapshotRevision   int64  `json:"snapshot_revision"`
	RolloutID          string `json:"rollout_id"`
	StageID            string `json:"stage_id"`
}

type createRolloutRequest struct {
	TenantID                 string               `json:"tenant_id"`
	Key                      string               `json:"key"`
	Environment              string               `json:"environment"`
	CandidateVersion         int                  `json:"candidate_version"`
	TargetServices           []string             `json:"target_services"`
	Stages                   []createRolloutStage `json:"stages"`
	RequiredAckPercentage    float64              `json:"required_ack_percentage"`
	DeploymentTimeoutSeconds int                  `json:"deployment_timeout_seconds"`
}

type createRolloutStage struct {
	ID                     string `json:"id"`
	Percentage             int    `json:"percentage"`
	MinimumDurationSeconds int    `json:"minimum_duration_seconds"`
}

type tenantResponse struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Owner       string `json:"owner,omitempty"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

type definitionResponse struct {
	ID           string          `json:"id"`
	TenantID     string          `json:"tenant_id"`
	Key          string          `json:"key"`
	Description  string          `json:"description,omitempty"`
	Schema       json.RawMessage `json:"schema"`
	DefaultValue json.RawMessage `json:"default,omitempty"`
	CreatedAt    string          `json:"created_at"`
	UpdatedAt    string          `json:"updated_at"`
}

type versionResponse struct {
	ID                 string          `json:"id"`
	ConfigDefinitionID string          `json:"config_definition_id"`
	VersionNumber      int             `json:"version_number"`
	Value              json.RawMessage `json:"value"`
	SourceCommitSHA    string          `json:"source_commit_sha,omitempty"`
	CreatedBy          string          `json:"created_by,omitempty"`
	CreatedAt          string          `json:"created_at"`
}

type environmentStateResponse struct {
	ConfigDefinitionID string `json:"config_definition_id"`
	Environment        string `json:"environment"`
	StableVersionID    string `json:"stable_version_id"`
	ActiveRolloutID    string `json:"active_rollout_id,omitempty"`
	UpdatedAt          string `json:"updated_at"`
}

type registerAgentResponse struct {
	Agent              agentResponse `json:"agent"`
	InstanceCredential string        `json:"instance_credential"`
	ExpiresAt          string        `json:"expires_at"`
}

type agentResponse struct {
	ID           string            `json:"id"`
	Service      string            `json:"service"`
	Environment  string            `json:"environment"`
	Zone         string            `json:"zone,omitempty"`
	Instance     string            `json:"instance"`
	Labels       map[string]string `json:"labels,omitempty"`
	RegisteredAt string            `json:"registered_at"`
	LastSeenAt   string            `json:"last_seen_at"`
}

type acknowledgementResponse struct {
	ID                 string              `json:"id"`
	AgentID            string              `json:"agent_id"`
	ConfigDefinitionID string              `json:"config_definition_id"`
	VersionID          string              `json:"version_id"`
	SnapshotRevision   int64               `json:"snapshot_revision"`
	Counted            bool                `json:"counted"`
	CreatedAt          string              `json:"created_at"`
	Rollout            *rolloutAckResponse `json:"rollout,omitempty"`
}

type rolloutAckResponse struct {
	Counted  bool             `json:"counted"`
	Decision string           `json:"decision"`
	Coverage coverageResponse `json:"coverage"`
}

type rolloutResponse struct {
	ID                 string           `json:"id"`
	ConfigDefinitionID string           `json:"config_definition_id"`
	TenantID           string           `json:"tenant_id"`
	Key                string           `json:"key"`
	Environment        string           `json:"environment"`
	StableVersionID    string           `json:"stable_version_id"`
	CandidateVersionID string           `json:"candidate_version_id"`
	CandidateVersion   int              `json:"candidate_version"`
	State              string           `json:"state"`
	CurrentStageID     string           `json:"current_stage_id"`
	Coverage           coverageResponse `json:"coverage"`
	Targets            int              `json:"targets"`
}

type coverageResponse struct {
	Total      int     `json:"total"`
	Acked      int     `json:"acked"`
	Percentage float64 `json:"percentage"`
}

type errorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

func tenantResponseFromDomain(tenant domain.Tenant) tenantResponse {
	return tenantResponse{
		ID:          tenant.ID,
		Name:        tenant.Name,
		Description: tenant.Description,
		Owner:       tenant.Owner,
		CreatedAt:   tenant.CreatedAt.Format(timeFormat),
		UpdatedAt:   tenant.UpdatedAt.Format(timeFormat),
	}
}

func definitionResponseFromDomain(definition domain.ConfigDefinition) definitionResponse {
	return definitionResponse{
		ID:           definition.ID,
		TenantID:     definition.TenantID,
		Key:          definition.Key,
		Description:  definition.Description,
		Schema:       definition.Schema,
		DefaultValue: definition.DefaultValue,
		CreatedAt:    definition.CreatedAt.Format(timeFormat),
		UpdatedAt:    definition.UpdatedAt.Format(timeFormat),
	}
}

func versionResponseFromDomain(version domain.ConfigVersion) versionResponse {
	return versionResponse{
		ID:                 version.ID,
		ConfigDefinitionID: version.ConfigDefinitionID,
		VersionNumber:      version.VersionNumber,
		Value:              version.Value,
		SourceCommitSHA:    version.SourceCommitSHA,
		CreatedBy:          version.CreatedBy,
		CreatedAt:          version.CreatedAt.Format(timeFormat),
	}
}

func environmentStateResponseFromDomain(state domain.ConfigEnvironmentState) environmentStateResponse {
	return environmentStateResponse{
		ConfigDefinitionID: state.ConfigDefinitionID,
		Environment:        string(state.Environment),
		StableVersionID:    state.StableVersionID,
		ActiveRolloutID:    state.ActiveRolloutID,
		UpdatedAt:          state.UpdatedAt.Format(timeFormat),
	}
}

func registerAgentResponseFromDomain(result agentregistry.RegisterResult) registerAgentResponse {
	return registerAgentResponse{
		Agent:              agentResponseFromDomain(result.Agent),
		InstanceCredential: result.Credential.Token,
		ExpiresAt:          result.Credential.ExpiresAt.Format(timeFormat),
	}
}

func agentResponseFromDomain(agent domain.Agent) agentResponse {
	return agentResponse{
		ID:           agent.ID,
		Service:      agent.Service,
		Environment:  string(agent.Environment),
		Zone:         agent.Zone,
		Instance:     agent.Instance,
		Labels:       agent.Labels,
		RegisteredAt: agent.RegisteredAt.Format(timeFormat),
		LastSeenAt:   agent.LastSeenAt.Format(timeFormat),
	}
}

func acknowledgementResponseFromDomain(ack domain.AgentAcknowledgement) acknowledgementResponse {
	return acknowledgementResponse{
		ID:                 ack.ID,
		AgentID:            ack.AgentID,
		ConfigDefinitionID: ack.ConfigDefinitionID,
		VersionID:          ack.VersionID,
		SnapshotRevision:   ack.SnapshotRevision,
		Counted:            ack.Counted,
		CreatedAt:          ack.CreatedAt.Format(timeFormat),
	}
}

func rolloutResponseFromDomain(rollout rollout.Rollout, targets []rollout.StageTarget, coverage rollout.Coverage) rolloutResponse {
	return rolloutResponse{
		ID:                 rollout.ID,
		ConfigDefinitionID: rollout.ConfigDefinitionID,
		TenantID:           rollout.TenantID,
		Key:                rollout.ConfigKey,
		Environment:        string(rollout.Environment),
		StableVersionID:    rollout.StableVersionID,
		CandidateVersionID: rollout.CandidateVersionID,
		CandidateVersion:   rollout.CandidateVersion,
		State:              string(rollout.State),
		CurrentStageID:     rollout.CurrentStageID,
		Coverage:           coverageResponseFromDomain(coverage),
		Targets:            len(targets),
	}
}

func coverageResponseFromDomain(coverage rollout.Coverage) coverageResponse {
	return coverageResponse{
		Total:      coverage.Total,
		Acked:      coverage.Acked,
		Percentage: coverage.Percentage,
	}
}

func stagesFromRequest(stages []createRolloutStage) []rollout.Stage {
	if len(stages) == 0 {
		return nil
	}

	result := make([]rollout.Stage, 0, len(stages))
	for _, stage := range stages {
		result = append(result, rollout.Stage{
			ID:              stage.ID,
			Percentage:      stage.Percentage,
			MinimumDuration: durationSeconds(stage.MinimumDurationSeconds),
		})
	}
	return result
}

func durationSeconds(seconds int) time.Duration {
	if seconds <= 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}

func bearerToken(header string) string {
	if header == "" {
		return ""
	}

	kind, token, ok := strings.Cut(header, " ")
	if !ok || !strings.EqualFold(kind, "Bearer") {
		return ""
	}
	return token
}

const timeFormat = "2006-01-02T15:04:05Z07:00"

package controlplane

import (
	"encoding/json"
	"errors"
	"net/http"

	"config-rollout-plane/internal/configregistry"
	"config-rollout-plane/internal/domain"
	"config-rollout-plane/internal/health"
)

type Handler struct {
	registry *configregistry.Service
}

func NewHandler(registry *configregistry.Service) http.Handler {
	h := Handler{registry: registry}

	mux := http.NewServeMux()
	healthHandler := health.NewHandler("control-plane", health.StaticChecker{})
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

const timeFormat = "2006-01-02T15:04:05Z07:00"

package dataplane

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"config-rollout-plane/internal/health"
)

type Handler struct {
	store    SnapshotStore
	verifier CredentialVerifier
}

func NewHandler(store SnapshotStore, verifier CredentialVerifier) http.Handler {
	h := Handler{
		store:    store,
		verifier: verifier,
	}

	mux := http.NewServeMux()
	healthHandler := health.NewHandler("data-plane", health.StaticChecker{})
	mux.Handle("/healthz", healthHandler)
	mux.Handle("/livez", healthHandler)
	mux.Handle("/readyz", healthHandler)
	mux.HandleFunc("GET /v1/agents/{agentID}/snapshot", h.getSnapshot)
	return mux
}

func (h Handler) getSnapshot(w http.ResponseWriter, r *http.Request) {
	requestedAgentID := r.PathValue("agentID")

	subjectAgentID, err := h.verifier.Verify(r.Context(), bearerToken(r.Header.Get("Authorization")))
	if err != nil {
		writeError(w, err)
		return
	}
	if subjectAgentID != requestedAgentID {
		writeError(w, ErrForbidden)
		return
	}

	snapshot, err := h.store.GetSnapshot(r.Context(), requestedAgentID)
	if err != nil {
		writeError(w, err)
		return
	}

	etag, err := snapshot.ETag()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{Error: "internal_error", Message: "snapshot etag failed"})
		return
	}

	w.Header().Set("ETag", etag)
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	writeJSON(w, http.StatusOK, snapshot)
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

func writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrMissingCredential), errors.Is(err, ErrInvalidCredential):
		writeJSON(w, http.StatusUnauthorized, errorResponse{Error: "unauthorized", Message: err.Error()})
	case errors.Is(err, ErrForbidden):
		writeJSON(w, http.StatusForbidden, errorResponse{Error: "forbidden", Message: err.Error()})
	case errors.Is(err, ErrSnapshotNotFound):
		writeJSON(w, http.StatusNotFound, errorResponse{Error: "not_found", Message: err.Error()})
	default:
		writeJSON(w, http.StatusInternalServerError, errorResponse{Error: "internal_error", Message: "request failed"})
	}
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

type errorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

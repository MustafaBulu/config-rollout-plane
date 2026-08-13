package agent

import (
	"encoding/json"
	"errors"
	"net/http"

	"config-rollout-plane/internal/health"
)

type Handler struct {
	cache *SnapshotCache
}

func NewHandler(cache *SnapshotCache) http.Handler {
	h := Handler{cache: cache}

	mux := http.NewServeMux()
	healthHandler := health.NewHandler("agent", cache)
	mux.Handle("/healthz", healthHandler)
	mux.Handle("/livez", healthHandler)
	mux.Handle("/readyz", healthHandler)
	mux.HandleFunc("GET /v1/snapshot", h.getSnapshot)
	mux.HandleFunc("GET /v1/config/{key}", h.getConfig)
	return mux
}

func (h Handler) getSnapshot(w http.ResponseWriter, r *http.Request) {
	snapshot, err := h.cache.Snapshot()
	if err != nil {
		writeAgentError(w, err)
		return
	}
	writeAgentJSON(w, http.StatusOK, snapshot)
}

func (h Handler) getConfig(w http.ResponseWriter, r *http.Request) {
	item, err := h.cache.Config(r.PathValue("key"))
	if err != nil {
		writeAgentError(w, err)
		return
	}

	writeAgentJSON(w, http.StatusOK, configResponse{
		Key:     item.Key,
		Version: item.Version,
		Value:   item.Value,
	})
}

func writeAgentError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNoSnapshot):
		writeAgentJSON(w, http.StatusServiceUnavailable, errorResponse{Error: "not_ready", Message: err.Error()})
	case errors.Is(err, ErrConfigNotFound):
		writeAgentJSON(w, http.StatusNotFound, errorResponse{Error: "not_found", Message: err.Error()})
	default:
		writeAgentJSON(w, http.StatusInternalServerError, errorResponse{Error: "internal_error", Message: "request failed"})
	}
}

func writeAgentJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

type configResponse struct {
	Key     string          `json:"key"`
	Version int             `json:"version"`
	Value   json.RawMessage `json:"value"`
}

type errorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

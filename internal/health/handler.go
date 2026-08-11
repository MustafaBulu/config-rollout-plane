package health

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

type Checker interface {
	Check(ctx context.Context) error
}

type StaticChecker struct{}

func (StaticChecker) Check(context.Context) error {
	return nil
}

type Handler struct {
	service string
	ready   Checker
}

func NewHandler(service string, ready Checker) http.Handler {
	if ready == nil {
		ready = StaticChecker{}
	}

	h := Handler{
		service: service,
		ready:   ready,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", h.handleHealth)
	mux.HandleFunc("GET /livez", h.handleHealth)
	mux.HandleFunc("GET /readyz", h.handleReady)
	return mux
}

func (h Handler) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, response{
		Service: h.service,
		Status:  "ok",
		Time:    time.Now().UTC(),
	})
}

func (h Handler) handleReady(w http.ResponseWriter, r *http.Request) {
	if err := h.ready.Check(r.Context()); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, response{
			Service: h.service,
			Status:  "not_ready",
			Time:    time.Now().UTC(),
			Error:   err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, response{
		Service: h.service,
		Status:  "ready",
		Time:    time.Now().UTC(),
	})
}

type response struct {
	Service string    `json:"service"`
	Status  string    `json:"status"`
	Time    time.Time `json:"time"`
	Error   string    `json:"error,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, payload response) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

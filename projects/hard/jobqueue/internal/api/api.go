// Package api is the producer-facing HTTP surface. Stdlib-only routing this
// time (Go 1.22+ method+path patterns) — compare with chi in shortlink.
package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/redis/go-redis/v9"

	"jobqueue/internal/job"
	"jobqueue/internal/queue"
)

type API struct {
	queue *queue.Queue
	log   *slog.Logger
}

func New(q *queue.Queue, log *slog.Logger) http.Handler {
	a := &API{queue: q, log: log}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /jobs", a.enqueue)
	mux.HandleFunc("GET /jobs/{id}", a.get)
	mux.HandleFunc("GET /stats", a.stats)
	return mux
}

type enqueueRequest struct {
	Type         string          `json:"type"`
	Payload      json.RawMessage `json:"payload"`
	Priority     job.Priority    `json:"priority,omitempty"`
	DelaySeconds int             `json:"delay_seconds,omitempty"`
	MaxAttempts  int             `json:"max_attempts,omitempty"`
}

func (a *API) enqueue(w http.ResponseWriter, r *http.Request) {
	var req enqueueRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Type == "" {
		respond(w, http.StatusBadRequest, map[string]string{"error": "type is required"})
		return
	}

	j, err := a.queue.Enqueue(r.Context(), req.Type, req.Payload, queue.EnqueueOptions{
		Priority:    req.Priority,
		Delay:       time.Duration(req.DelaySeconds) * time.Second,
		MaxAttempts: req.MaxAttempts,
	})
	if err != nil {
		a.log.Error("enqueue", "err", err)
		respond(w, http.StatusInternalServerError, map[string]string{"error": "enqueue failed"})
		return
	}
	respond(w, http.StatusAccepted, j)
}

func (a *API) get(w http.ResponseWriter, r *http.Request) {
	j, err := a.queue.Job(r.Context(), r.PathValue("id"))
	if errors.Is(err, redis.Nil) {
		respond(w, http.StatusNotFound, map[string]string{"error": "no such job"})
		return
	}
	if err != nil {
		respond(w, http.StatusInternalServerError, map[string]string{"error": "lookup failed"})
		return
	}
	respond(w, http.StatusOK, j)
}

func (a *API) stats(w http.ResponseWriter, r *http.Request) {
	s, err := a.queue.Stats(r.Context())
	if err != nil {
		respond(w, http.StatusInternalServerError, map[string]string{"error": "stats failed"})
		return
	}
	respond(w, http.StatusOK, s)
}

func respond(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

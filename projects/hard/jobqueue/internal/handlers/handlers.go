// Package handlers holds the demo job implementations.
package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"jobqueue/internal/job"
	"jobqueue/internal/worker"
)

// RegisterAll wires every known job type into the pool.
func RegisterAll(p *worker.Pool, log *slog.Logger) {
	p.Register("email", email(log))
	p.Register("resize", resize(log))
	p.Register("flaky", flaky(log))
	p.Register("panic", panicky())
}

func email(log *slog.Logger) worker.Handler {
	return func(ctx context.Context, j *job.Job) error {
		var p struct {
			To      string `json:"to"`
			Subject string `json:"subject"`
		}
		if err := json.Unmarshal(j.Payload, &p); err != nil {
			return fmt.Errorf("bad payload: %w", err)
		}
		select { // simulate SMTP latency, but stay cancellable
		case <-time.After(200 * time.Millisecond):
		case <-ctx.Done():
			return ctx.Err()
		}
		log.Info("email sent", "to", p.To, "subject", p.Subject)
		return nil
	}
}

func resize(log *slog.Logger) worker.Handler {
	return func(ctx context.Context, j *job.Job) error {
		select {
		case <-time.After(400 * time.Millisecond):
		case <-ctx.Done():
			return ctx.Err()
		}
		log.Info("image resized", "job", j.ID)
		return nil
	}
}

// flaky fails until the 3rd attempt — run it and watch the backoff schedule
// play out in the logs, ending in success (or in q:dead if max_attempts < 3).
func flaky(log *slog.Logger) worker.Handler {
	return func(_ context.Context, j *job.Job) error {
		if j.Attempts < 3 {
			return fmt.Errorf("flaky failure on attempt %d", j.Attempts)
		}
		log.Info("flaky job finally succeeded", "attempt", j.Attempts)
		return nil
	}
}

// panicky proves the pool's recovery: a panic becomes a failed attempt.
func panicky() worker.Handler {
	return func(context.Context, *job.Job) error {
		panic("handler bug!")
	}
}

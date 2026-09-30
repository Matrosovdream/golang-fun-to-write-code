// Package worker executes jobs from the queue: a pool with per-job
// timeouts, panic recovery, heartbeats and graceful drain.
package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"jobqueue/internal/job"
	"jobqueue/internal/queue"
)

// Handler processes one job. Because delivery is at-least-once (see
// queue.Reap), handlers MUST be idempotent.
type Handler func(ctx context.Context, j *job.Job) error

type Pool struct {
	queue      *queue.Queue
	handlers   map[string]Handler
	log        *slog.Logger
	size       int
	jobTimeout time.Duration
	pollEvery  time.Duration
}

func NewPool(q *queue.Queue, log *slog.Logger, size int) *Pool {
	return &Pool{
		queue:      q,
		handlers:   make(map[string]Handler),
		log:        log,
		size:       size,
		jobTimeout: 30 * time.Second,
		pollEvery:  100 * time.Millisecond,
	}
}

func (p *Pool) Register(jobType string, h Handler) { p.handlers[jobType] = h }

// Run blocks until ctx is cancelled, then drains: every worker finishes its
// current job before returning. It also runs the scheduler (delayed →
// pending) and the reaper (crashed workers) as background loops.
func (p *Pool) Run(ctx context.Context) {
	var wg sync.WaitGroup

	wg.Add(2)
	go func() { defer wg.Done(); p.loop(ctx, 500*time.Millisecond, p.schedulerTick) }()
	go func() { defer wg.Done(); p.loop(ctx, 5*time.Second, p.reaperTick) }()

	for i := range p.size {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p.worker(ctx, i)
		}()
	}
	wg.Wait()
	p.log.Info("pool drained")
}

func (p *Pool) worker(ctx context.Context, id int) {
	for {
		j, err := p.queue.Dequeue(ctx)
		switch {
		case errors.Is(err, queue.ErrEmpty):
			// Idle poll — cheap and simple. Exercise: replace with a
			// blocking BLMove per priority and compare.
			select {
			case <-time.After(p.pollEvery):
				continue
			case <-ctx.Done():
				return
			}
		case err != nil:
			if ctx.Err() != nil {
				return
			}
			p.log.Error("dequeue", "worker", id, "err", err)
			continue
		}

		p.execute(ctx, id, j)

		// Drain point: only between jobs, never mid-job.
		if ctx.Err() != nil {
			return
		}
	}
}

func (p *Pool) execute(parent context.Context, workerID int, j *job.Job) {
	p.log.Info("job start", "worker", workerID, "id", j.ID, "type", j.Type, "attempt", j.Attempts)

	// context.WithoutCancel: a drain (parent cancel) must not abort a job
	// already in flight — it finishes, then the worker exits.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), p.jobTimeout)
	defer cancel()

	stopBeat := p.startHeartbeat(ctx, j.ID)
	err := p.runHandler(ctx, j)
	stopBeat()

	if err != nil {
		p.log.Warn("job failed", "id", j.ID, "attempt", j.Attempts, "max", j.MaxAttempts, "err", err)
		if ferr := p.queue.Fail(ctx, j, err); ferr != nil {
			p.log.Error("fail bookkeeping", "id", j.ID, "err", ferr)
		}
		return
	}
	if aerr := p.queue.Ack(ctx, j); aerr != nil {
		p.log.Error("ack", "id", j.ID, "err", aerr)
		return
	}
	p.log.Info("job done", "worker", workerID, "id", j.ID, "type", j.Type)
}

// runHandler isolates the panic recovery so a panicking handler counts as a
// failed attempt instead of killing the worker.
func (p *Pool) runHandler(ctx context.Context, j *job.Job) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("handler panicked: %v", r)
		}
	}()

	h, ok := p.handlers[j.Type]
	if !ok {
		return fmt.Errorf("no handler registered for type %q", j.Type)
	}
	return h(ctx, j)
}

func (p *Pool) startHeartbeat(ctx context.Context, jobID string) (stop func()) {
	hbCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := p.queue.Heartbeat(hbCtx, jobID); err != nil {
					p.log.Warn("heartbeat", "id", jobID, "err", err)
				}
			case <-hbCtx.Done():
				return
			}
		}
	}()
	return func() { cancel(); <-done }
}

func (p *Pool) loop(ctx context.Context, every time.Duration, tick func(context.Context)) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			tick(ctx)
		case <-ctx.Done():
			return
		}
	}
}

func (p *Pool) schedulerTick(ctx context.Context) {
	if n, err := p.queue.MoveDueJobs(ctx); err != nil && ctx.Err() == nil {
		p.log.Error("scheduler", "err", err)
	} else if n > 0 {
		p.log.Info("promoted delayed jobs", "count", n)
	}
}

func (p *Pool) reaperTick(ctx context.Context) {
	if n, err := p.queue.Reap(ctx); err != nil && ctx.Err() == nil {
		p.log.Error("reaper", "err", err)
	} else if n > 0 {
		p.log.Warn("requeued jobs from crashed workers", "count", n)
	}
}

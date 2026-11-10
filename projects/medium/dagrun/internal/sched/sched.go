// Package sched runs a dag.Graph with bounded parallelism.
//
// Design: the goroutine that calls Run is the coordinator. It alone owns the
// scheduling state (indegrees, the ready queue, what has failed), so that state
// needs no locks. It talks to a fixed pool of Jobs workers over two unbuffered
// channels:
//
//	coordinator --ready--> workers --done--> coordinator
//
// A task counts as launched the instant a worker receives it, and "stop" simply
// means the coordinator stops offering work. That makes "nothing new starts
// after a failure" exact, with no shared flag for workers to race on, and the
// pool size is the -j limit by construction.
package sched

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"dagrun/internal/dag"
)

// Executor runs one task, writing its combined stdout and stderr to out.
type Executor interface {
	Run(ctx context.Context, t dag.Task, out io.Writer) error
}

type Kind int

const (
	Started Kind = iota
	Succeeded
	Failed
	Skipped
)

type Event struct {
	Kind   Kind
	Task   string
	Dur    time.Duration // Succeeded, Failed
	Err    error         // Failed
	Reason string        // Skipped
}

type Scheduler struct {
	Exec      Executor
	Jobs      int                         // tasks running at once; values below 1 mean 1
	KeepGoing bool                        // after a failure, still run tasks that do not depend on it
	Output    func(task string) io.Writer // sink for a task's output, called from workers; nil discards
	Observe   func(Event)                 // must be safe for concurrent use; nil ignores
}

type result struct {
	task string
	dur  time.Duration
	err  error
}

// Run returns nil only if every task succeeded. Otherwise it returns the
// failures joined, plus ctx.Err() if cancellation cut the run short.
func (s *Scheduler) Run(ctx context.Context, g *dag.Graph) error {
	observe, output := s.Observe, s.Output
	if observe == nil {
		observe = func(Event) {}
	}
	if output == nil {
		output = func(string) io.Writer { return io.Discard }
	}

	ready, done := make(chan string), make(chan result)
	var wg sync.WaitGroup
	for range max(s.Jobs, 1) {
		wg.Go(func() {
			for name := range ready {
				// Emitted here, not by the coordinator, so "started" is always
				// ordered before anything the task prints.
				observe(Event{Kind: Started, Task: name})
				start := time.Now()
				err := s.Exec.Run(ctx, g.Task(name), output(name))
				done <- result{name, time.Since(start), err}
			}
		})
	}

	indeg := make(map[string]int, len(g.Names()))
	var queue []string
	for _, n := range g.Names() {
		if indeg[n] = len(g.Task(n).Deps); indeg[n] == 0 {
			queue = append(queue, n)
		}
	}

	var (
		inflight int
		stop     bool
		failures []error
		launched = map[string]bool{}
		skipped  = map[string]bool{}
	)
	skip := func(name, reason string) {
		skipped[name] = true
		observe(Event{Kind: Skipped, Task: name, Reason: reason})
	}

	ctxDone := ctx.Done()
	for {
		stop = stop || ctx.Err() != nil
		if inflight == 0 && (stop || len(queue) == 0) {
			break
		}
		// Nil-channel trick: with nothing to offer (or told to stop) the send
		// case blocks forever, so the select only waits for results.
		var send chan string
		var next string
		if !stop && len(queue) > 0 {
			send, next = ready, queue[0]
		}
		select {
		case send <- next:
			queue = queue[1:]
			launched[next] = true
			inflight++
		case r := <-done:
			inflight--
			if r.err == nil {
				observe(Event{Kind: Succeeded, Task: r.task, Dur: r.dur})
				for _, d := range g.Dependents(r.task) {
					if indeg[d]--; indeg[d] == 0 {
						queue = append(queue, d)
					}
				}
			} else {
				failures = append(failures, fmt.Errorf("%s: %w", r.task, r.err))
				observe(Event{Kind: Failed, Task: r.task, Dur: r.dur, Err: r.err})
				// The failed task never decrements its dependents, so they would
				// simply never become ready; skipping them explicitly just says so.
				for _, d := range g.Downstream(r.task) {
					if !skipped[d] {
						skip(d, fmt.Sprintf("dependency %q failed", r.task))
					}
				}
				stop = stop || !s.KeepGoing
			}
		case <-ctxDone:
			ctxDone = nil // a closed channel is always ready; fire once, then wait on results only
		}
	}
	close(ready)
	wg.Wait()

	reason := "stopped after a failure"
	if ctx.Err() != nil {
		reason = "canceled"
	}
	aborted := false
	for _, n := range g.Names() {
		if !launched[n] && !skipped[n] {
			skip(n, reason)
			aborted = true
		}
	}
	if aborted && ctx.Err() != nil {
		failures = append(failures, ctx.Err())
	}
	return errors.Join(failures...)
}

// Package render turns scheduler events and task output into the terminal view.
package render

import (
	"bytes"
	"fmt"
	"io"
	"sync"
	"time"

	"dagrun/internal/sched"
)

// Renderer is the observer: it consumes sched events and hands out the
// per-task writers that prefix output with "[task] ". Everything funnels
// through one mutex and one Fprintf per line, so lines never interleave.
type Renderer struct {
	mu      sync.Mutex // guards everything below
	out     io.Writer
	width   int
	writers map[string]*lineWriter
	counts  map[sched.Kind]int
}

func New(out io.Writer, tasks []string) *Renderer {
	width := 0
	for _, t := range tasks {
		width = max(width, len(t))
	}
	return &Renderer{out: out, width: width, writers: map[string]*lineWriter{}, counts: map[sched.Kind]int{}}
}

// Writer returns the sink for one task's output. Safe to call concurrently.
func (r *Renderer) Writer(task string) io.Writer {
	r.mu.Lock()
	defer r.mu.Unlock()
	w := &lineWriter{r: r, task: task}
	r.writers[task] = w
	return w
}

// Observe is safe for concurrent use.
func (r *Renderer) Observe(e sched.Event) {
	r.mu.Lock()
	r.counts[e.Kind]++
	r.mu.Unlock()
	switch e.Kind {
	case sched.Started:
		r.emit(e.Task, "started")
	case sched.Succeeded:
		r.flush(e.Task)
		r.emit(e.Task, fmt.Sprintf("ok (%s)", e.Dur.Round(time.Millisecond)))
	case sched.Failed:
		r.flush(e.Task)
		r.emit(e.Task, fmt.Sprintf("FAILED (%s): %v", e.Dur.Round(time.Millisecond), e.Err))
	case sched.Skipped:
		r.emit(e.Task, "skipped: "+e.Reason)
	}
}

func (r *Renderer) Summary() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return fmt.Sprintf("%d ok, %d failed, %d skipped",
		r.counts[sched.Succeeded], r.counts[sched.Failed], r.counts[sched.Skipped])
}

// emit writes one finished line; a terminal that went away is not our problem.
func (r *Renderer) emit(task, msg string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	fmt.Fprintf(r.out, "[%-*s] %s\n", r.width, task, msg)
}

func (r *Renderer) flush(task string) {
	r.mu.Lock()
	w := r.writers[task]
	r.mu.Unlock()
	if w != nil && len(w.buf) > 0 {
		r.emit(task, string(w.buf))
		w.buf = nil
	}
}

// lineWriter buffers partial writes and emits only complete lines, so a task
// that prints "50%... " now and "done\n" later still produces one clean line.
// It is used by one goroutine at a time (os/exec guarantees that when stdout
// and stderr share a writer).
type lineWriter struct {
	r    *Renderer
	task string
	buf  []byte
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	for {
		line, rest, ok := bytes.Cut(w.buf, []byte{'\n'})
		if !ok {
			return len(p), nil
		}
		w.r.emit(w.task, string(line))
		w.buf = rest
	}
}

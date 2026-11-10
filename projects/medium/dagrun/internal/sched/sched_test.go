package sched_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"dagrun/internal/dag"
	"dagrun/internal/sched"
)

var errBoom = errors.New("boom")

// step scripts one task: how long it "runs" and whether it then fails.
type step struct {
	dur time.Duration
	err error
}

// fake is the scripted Executor. It runs inside synctest bubbles, so its
// latencies are fake time: tasks overlap deterministically and the whole file
// runs in microseconds.
type fake struct {
	steps map[string]step

	mu         sync.Mutex
	succeeded  map[string]bool
	started    []string
	violations []string
	running    int
	highWater  int
}

func newFake(steps map[string]step) *fake {
	return &fake{steps: steps, succeeded: map[string]bool{}}
}

func (f *fake) Run(ctx context.Context, t dag.Task, out io.Writer) error {
	f.mu.Lock()
	for _, d := range t.Deps {
		if !f.succeeded[d] {
			f.violations = append(f.violations, fmt.Sprintf("%s started before %s succeeded", t.Name, d))
		}
	}
	f.started = append(f.started, t.Name)
	f.running++
	f.highWater = max(f.highWater, f.running)
	f.mu.Unlock()

	fmt.Fprintf(out, "output of %s\n", t.Name)
	st := f.steps[t.Name]
	if st.dur == 0 {
		st.dur = time.Second
	}
	var err error
	select {
	case <-time.After(st.dur):
		err = st.err
	case <-ctx.Done():
		err = ctx.Err()
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	f.running--
	f.succeeded[t.Name] = err == nil
	return err
}

// graphOf builds tasks from "name: dep dep" shorthand.
func graphOf(t *testing.T, spec ...string) *dag.Graph {
	t.Helper()
	tasks := map[string]dag.Task{}
	for _, s := range spec {
		name, deps, _ := strings.Cut(s, ":")
		tasks[name] = dag.Task{Cmd: "true", Deps: strings.Fields(deps)}
	}
	g, err := dag.New(tasks)
	require.NoError(t, err)
	return g
}

type run struct {
	events []sched.Event
	err    error
}

func (r run) of(k sched.Kind) []string {
	var out []string
	for _, e := range r.events {
		if e.Kind == k {
			out = append(out, e.Task)
		}
	}
	slices.Sort(out)
	return out
}

func (r run) index(k sched.Kind, task string) int {
	return slices.IndexFunc(r.events, func(e sched.Event) bool { return e.Kind == k && e.Task == task })
}

func exec(ctx context.Context, g *dag.Graph, f *fake, jobs int, keepGoing bool) run {
	var (
		mu sync.Mutex // Started events come from workers, the rest from the coordinator
		r  run
	)
	s := sched.Scheduler{Exec: f, Jobs: jobs, KeepGoing: keepGoing, Observe: func(e sched.Event) {
		mu.Lock()
		defer mu.Unlock()
		r.events = append(r.events, e)
	}}
	r.err = s.Run(ctx, g)
	return r
}

func TestJobsLimitIsNeverExceeded(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g := graphOf(t, "t0:", "t1:", "t2:", "t3:", "t4:", "t5:", "t6:", "t7:", "t8:",
			"end: t0 t1 t2 t3 t4 t5 t6 t7 t8")
		f := newFake(nil)
		start := time.Now()

		r := exec(t.Context(), g, f, 3, false)

		require.NoError(t, r.err)
		require.Len(t, r.of(sched.Succeeded), 10)
		require.Equal(t, 3, f.highWater, "saturates the limit but never passes it")
		// Three rounds of three one-second tasks, then end.
		require.Equal(t, 4*time.Second, time.Since(start))
		require.Empty(t, f.violations)
	})
}

func TestDependenciesFinishBeforeDependentsStart(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g := graphOf(t, "gen:", "lint:", "build: gen", "test: build", "docs: gen", "deploy: test lint docs")
		f := newFake(map[string]step{
			"gen": {dur: 3 * time.Second}, "lint": {dur: 20 * time.Second}, "build": {dur: 5 * time.Second},
			"test": {dur: 2 * time.Second}, "docs": {dur: 9 * time.Second}, "deploy": {dur: time.Second},
		})

		r := exec(t.Context(), g, f, 8, false)

		require.NoError(t, r.err)
		require.Empty(t, f.violations)
		for _, n := range g.Names() {
			for _, d := range g.Task(n).Deps {
				require.Less(t, r.index(sched.Succeeded, d), r.index(sched.Started, n), "%s before %s", d, n)
			}
		}
		require.Equal(t, 3, f.highWater, "lint is still running when build and docs start after gen")
	})
}

func TestFailureStopsLaunchingAndSkipsEverythingNotStarted(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g := graphOf(t, "a:", "b:", "c: a", "d: c b", "e: b", "f: e")
		f := newFake(map[string]step{"a": {dur: 10 * time.Millisecond, err: errBoom}, "b": {dur: time.Second}})

		r := exec(t.Context(), g, f, 4, false)

		require.ErrorIs(t, r.err, errBoom)
		require.ErrorContains(t, r.err, "a: boom")
		require.ElementsMatch(t, []string{"a", "b"}, f.started, "b was already running; nothing launches after a fails")
		require.Equal(t, []string{"b"}, r.of(sched.Succeeded), "running tasks are left to finish")
		require.Equal(t, []string{"a"}, r.of(sched.Failed))
		require.Equal(t, []string{"c", "d", "e", "f"}, r.of(sched.Skipped))
		for _, e := range r.events {
			switch e.Task {
			case "c", "d":
				require.Equal(t, `dependency "a" failed`, e.Reason)
			case "e", "f":
				require.Equal(t, "stopped after a failure", e.Reason)
			}
		}
	})
}

func TestKeepGoingSkipsExactlyTheTransitiveDependents(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g := graphOf(t, "a:", "b:", "c: a", "d: c b", "e: b", "f: e")
		f := newFake(map[string]step{"a": {dur: 10 * time.Millisecond, err: errBoom}, "b": {dur: time.Second}})

		r := exec(t.Context(), g, f, 4, true)

		require.ErrorIs(t, r.err, errBoom)
		require.Equal(t, []string{"a"}, r.of(sched.Failed))
		require.Equal(t, []string{"c", "d"}, r.of(sched.Skipped), "d is skipped through c even though b succeeded")
		require.Equal(t, []string{"b", "e", "f"}, r.of(sched.Succeeded))
		require.ElementsMatch(t, []string{"a", "b", "e", "f"}, f.started)
		require.Empty(t, f.violations)
	})
}

func TestCancelStopsEverything(t *testing.T) {
	g := graphOf(t, "a:", "b:", "c: a", "d: b")

	t.Run("while running", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			f := newFake(map[string]step{"a": {dur: time.Hour}, "b": {dur: time.Hour}})
			go func() {
				time.Sleep(time.Second)
				cancel()
			}()

			r := exec(ctx, g, f, 2, true)

			require.ErrorIs(t, r.err, context.Canceled)
			require.Equal(t, []string{"a", "b"}, r.of(sched.Failed))
			require.Equal(t, []string{"c", "d"}, r.of(sched.Skipped))
			require.ElementsMatch(t, []string{"a", "b"}, f.started)
			require.Zero(t, f.running)
		})
	})

	t.Run("before starting", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		f := newFake(nil)

		r := exec(ctx, g, f, 2, false)

		require.ErrorIs(t, r.err, context.Canceled)
		require.Empty(t, f.started)
		require.Equal(t, []string{"a", "b", "c", "d"}, r.of(sched.Skipped))
	})
}

func TestOutputIsRoutedPerTaskAndStartedComesFirst(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var mu sync.Mutex
		var log []string
		bufs := map[string]*strings.Builder{}
		s := sched.Scheduler{
			Exec: newFake(nil),
			Jobs: 2,
			Output: func(task string) io.Writer {
				mu.Lock()
				defer mu.Unlock()
				bufs[task] = &strings.Builder{}
				return writerFunc(func(p []byte) {
					mu.Lock()
					defer mu.Unlock()
					log = append(log, "output "+task)
					bufs[task].Write(p)
				})
			},
			Observe: func(e sched.Event) {
				if e.Kind == sched.Started {
					mu.Lock()
					defer mu.Unlock()
					log = append(log, "started "+e.Task)
				}
			},
		}

		require.NoError(t, s.Run(t.Context(), graphOf(t, "x:", "y: x")))

		require.Equal(t, "output of x\n", bufs["x"].String())
		require.Equal(t, "output of y\n", bufs["y"].String())
		require.Equal(t, []string{"started x", "output x", "started y", "output y"}, log)
	})
}

type writerFunc func([]byte)

func (f writerFunc) Write(p []byte) (int, error) { f(p); return len(p), nil }

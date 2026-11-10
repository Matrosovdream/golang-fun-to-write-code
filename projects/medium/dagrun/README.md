# dagrun — a tiny `make` / CI engine

`dagrun -f dagrun.yaml -j 4 build test` reads a YAML task graph, runs independent tasks in
parallel (at most `-j` at once), streams their output as `[task] line`, skips the dependents of
a failed task (`-k` keeps going with everything else), and kills whole child process trees on
Ctrl+C. `--dry-run` prints the plan level by level, `--dot` prints Graphviz.

```yaml
tasks:
  build: {cmd: "go build ./...", deps: [gen], env: {CGO_ENABLED: "0"}, dir: .}
```

Per task: `cmd` (run with `sh -c`), `deps`, `env` (added to the inherited environment), `dir`
(relative to where you run dagrun). Unknown YAML fields are an error, so a typo like `dep:` cannot
silently drop an edge. Flags go before task names; no task names means "everything".

## What this project teaches

- **Indegree-counting scheduler** (`internal/sched/sched.go`, `Run`): one coordinator goroutine owns
  all scheduling state (indegrees, ready queue, failures), so it needs no locks. It feeds a fixed pool
  of `-j` workers over an unbuffered `ready` channel and collects results on `done`. A task is
  "launched" the instant a worker receives it, so `-j` is enforced by construction and "nothing new
  starts after a failure" is exact. The send case uses the nil-channel trick (same as `fetchpool`):
  with nothing to offer, or after a stop decision, it blocks forever and the `select` only waits for results.
- **Failure semantics** (same function): the default stops launching and skips everything not yet
  started; `-k` skips only `g.Downstream(failed)` and keeps going. Running tasks are never killed by
  a sibling's failure, only by cancellation.
- **Kahn's algorithm, one level at a time** (`internal/dag/graph.go`, `kahn`): produces `--dry-run`'s
  levels, and whatever never reaches indegree 0 is the "stuck" set that proves a cycle.
- **Reporting the cycle, not just detecting it** (`cycle`): every stuck task has a stuck dependency, so
  walking "first stuck dependency" must revisit a task; that slice is the cycle, printed as
  `cycle: a -> b -> c -> a`. An arrow always means "depends on". Unknown dependency names are a separate,
  typed error (`UnknownDepError`), all reported together.
- **Transitive closure** (`closure`, `Select`, `Downstream`): one DFS serves both "run only what
  `deploy` needs" (walk deps) and "what is lost if this fails" (walk dependents).
- **Strategy** (`sched.Executor`): the scheduler consumes a one-method interface; tests use a scripted
  fake, `internal/osexec` is the real `os/exec` implementation.
- **Observer** (`sched.Event`, `internal/render`): the scheduler emits events, the renderer turns them
  into terminal lines and hands out the per-task writers.
- **A line-prefixing `io.Writer` that never garbles** (`render.lineWriter`): buffers partial writes,
  emits only whole lines, and every line goes out in one `Fprintf` under one mutex, so two tasks can
  never interleave mid-line. Stdout and stderr share one writer, which makes `os/exec` copy both from a
  single goroutine and keeps their relative order.
- **Killing a process tree** (`internal/osexec`): `Setpgid` puts each task in its own process group,
  `cmd.Cancel` sends SIGTERM to `-pgid`, `cmd.WaitDelay` stops `Wait` hanging when a grandchild still
  holds the output pipe, and after a cancelled run a final SIGKILL to the group sweeps anything that
  ignored SIGTERM (`WaitDelay` itself only kills the shell). The unix bits live behind `//go:build unix`
  with a no-op fallback so the package still compiles on Windows.
- **Testing concurrency without sleeping** (`internal/sched/sched_test.go`): the fake executor runs
  inside `testing/synctest` bubbles, so "tasks take 1s" is fake time. That makes the `-j` high-water mark
  exactly equal to the limit (not merely `<=`), and lets the test assert total virtual wall time.
  `internal/dag` also has a seeded random-graph property test: every reported cycle is a real cycle, and
  every acyclic graph's levels respect every edge.
- **One real-process test** (`internal/osexec/osexec_unix_test.go`): runs `sh -c 'sleep 30 & ...; wait'`,
  cancels, and checks with `kill -0` that the grandchild is gone, once with a cooperative tree (the
  grandchild leaves proof that it got SIGTERM) and once with a tree that ignores SIGTERM.

## Run

```sh
go test -race ./...

go build -o bin/dagrun ./cmd/dagrun       # bin/ is git-ignored

bin/dagrun --dry-run deploy               # the plan, level by level
bin/dagrun --dot package                  # Graphviz; pipe into `dot -Tsvg` if you have it
bin/dagrun -j 4 package                   # parallel: finishes in about 5s
bin/dagrun -j 1 package                   # same graph, serial: about 7s
bin/dagrun -j 4 deploy                    # `flaky` fails: running tasks finish, test/package never start, exit 1
bin/dagrun -k -j 4 deploy                 # -k: test and package still run; only integration and deploy are skipped, exit 1

# Ctrl+C, scripted: no `sleep 300` survives
(bin/dagrun hang & pid=$!; sleep 1; kill -INT $pid; wait $pid); pgrep -f 'sleep 300' || echo "no stragglers"
```

Measured on this machine with the demo `dagrun.yaml` (the wall time dagrun prints in its own
summary line): `-j 4 package` 5.03-5.06s, `-j 1 package` 7.06-7.10s over four runs. The critical path is
fetch (1s) -> compile-a (2s) -> test (1s) -> package (1s). Config errors name the problem:

```sh
printf 'tasks:\n  a: {cmd: x, deps: [b]}\n  b: {cmd: x, deps: [c]}\n  c: {cmd: x, deps: [a]}\n' > bin/cycle.yaml
bin/dagrun -f bin/cycle.yaml    # dagrun: bin/cycle.yaml: cycle: a -> b -> c -> a
bin/dagrun nope                 # dagrun: unknown task: "nope"
```

## Rewrite exercises

1. Rewrite `Scheduler.Run` from memory: coordinator, `ready`/`done` channels, the nil-channel send, and
   the stop rules. Then break the `inflight` accounting and watch what deadlocks.
2. Replace the worker pool with `golang.org/x/sync/errgroup` + `SetLimit` and one goroutine per ready
   task. What becomes harder: the `-k` / stop semantics, or the high-water test?
3. Add `timeout: 30s` per task via `context.WithTimeout` inside the executor, and test it with the
   fake executor under `synctest`.
4. Add up-to-date checks like `make`: `inputs:` / `outputs:` globs, skip a task whose outputs are
   newer than its inputs. Emit a `Skipped` event with reason `up to date`.
5. Replace the `Cancel` + `WaitDelay` + sweep combination with your own two-phase shutdown (SIGTERM,
   wait, SIGKILL on a timer). Which pid-reuse race did you just introduce?

## Deliberately left out

- Windows process-tree killing (the fallback only kills the shell) and any non-`sh` shell.
- Colors, a TTY progress UI, per-task timeouts, retries, caching.
- Flags after task names (`dagrun deploy -k` treats `-k` as a task name and fails with "unknown task").
- Validating `--dot` output with the real `dot` binary: it is not installed here; the output is
  covered by exact-string tests only.

# jobqueue — background job processing system

A Sidekiq/asynq-style system built from first principles on Redis: producer API, worker
fleet, priorities, delayed jobs, retries with exponential backoff, dead-letter queue,
crash recovery, scheduled (cron) jobs.

## What this project teaches

- **The reliable queue pattern**: `LMOVE pending → processing` + a lease key with TTL.
  A plain `RPOP` loses jobs when a worker dies; read `Dequeue`'s comment.
- **At-least-once + idempotency**: the reaper re-delivers jobs whose worker crashed —
  which is exactly why handlers must be idempotent. The chaos test simulates the crash.
- **Delayed jobs and retry backoff share one mechanism**: a sorted set scored by run-at
  time, promoted by a scheduler tick (`ZRangeByScore` → `ZRem` → `LPush`, with the
  `ZRem`-wins race rule for multiple schedulers).
- **Explicit state machine**: pending → running → done/pending(retry)/dead, with a
  transitions table that rejects illegal moves.
- **Worker pool discipline**: per-job timeout, `context.WithoutCancel` so drain doesn't
  kill in-flight jobs, panic recovery counting as a failed attempt, heartbeats extending
  the lease for long jobs.
- **go-redis v9** (lists, zsets, leases) and **miniredis** — full-fidelity Redis unit
  tests with zero infrastructure.
- **robfig/cron**: recurring jobs enqueue like everything else; cron never executes work.
- **Stdlib 1.22+ routing** (`POST /jobs`, `{id}` path values) — contrast with chi earlier.

## Run

```sh
go test ./...            # miniredis: no Docker needed (takes ~15s, timing tests)

# demo, one process, embedded redis:
go run ./cmd/jobqueue -embedded

# in another terminal:
curl -s -X POST localhost:8087/jobs -d '{"type":"email","payload":{"to":"a@b.co","subject":"hi"}}'
curl -s -X POST localhost:8087/jobs -d '{"type":"flaky"}'                  # watch retries in the logs
curl -s -X POST localhost:8087/jobs -d '{"type":"panic","max_attempts":1}' # dead-letter demo
curl -s -X POST localhost:8087/jobs -d '{"type":"resize","delay_seconds":5}'
curl -s localhost:8087/stats

# scale-out shape (real Redis):
docker run --rm -p 6379:6379 redis:7-alpine
go run ./cmd/jobqueue -mode api
go run ./cmd/jobqueue -mode worker   # × N processes
```

## Rewrite exercises

1. Rebuild `queue.go` from the data-layout comment at the top of the file.
2. Replace polling `Dequeue` with blocking `BLMove` (one per priority? one goroutine each?
   what happens to strict priority?).
3. Make `MoveDueJobs` a Lua script so promote is one atomic round trip.
4. Add unique jobs: an idempotency key that rejects duplicate enqueues for 24h (SETNX).
5. Add a `q:paused` flag the workers respect — pause/resume processing at runtime.

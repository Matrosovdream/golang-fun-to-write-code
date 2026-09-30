# fetchpool — concurrent link checker & crawler

Two modes, two canonical concurrency shapes:

- **check** (`-file urls.txt`): a known, flat work list → `errgroup` + `SetLimit`.
- **crawl** (`<start-url>`): the work list *grows while you process it* → worker pool +
  single-goroutine coordinator.

## What this project teaches

- **Worker pool** over an unbuffered `jobs` channel; workers exit when it closes.
- **Coordinator pattern**: the visited set and queue live in *one* goroutine — no mutex.
  "Share memory by communicating."
- **The nil-channel trick**: setting the send channel to `nil` disables that `select` case
  while the queue is empty. Read `Crawl` until this clicks — it's the heart of the project.
- **Termination**: `pending` counts queued + in-flight jobs; zero means the crawl is done.
  Getting this wrong deadlocks or exits early — try breaking it and watch.
- **errgroup**: `SetLimit` as a semaphore; ctx cancellation propagating to every worker.
- **Rate limiting**: one shared `x/time/rate` token bucket across all workers.
- **Cancellation end-to-end**: Ctrl+C → `signal.NotifyContext` → `http.NewRequestWithContext`
  → in-flight requests abort.
- **sync/atomic counters** where goroutines share tallies; index-partitioned slice writes
  where they don't need any locking at all.
- **HTML tokenizing** with `x/net/html`, resolving relative URLs against the page URL.
- **Race testing**: run `go test -race ./...` — keep it green while you rewrite.

## Run

```sh
go test -race ./...

go run ./cmd/fetchpool -depth 1 https://go.dev/
printf 'https://go.dev/\nhttps://go.dev/nope\n' > /tmp/urls.txt
go run ./cmd/fetchpool -file /tmp/urls.txt
```

## Rewrite exercises

1. Rewrite `Crawl` from memory. The termination condition is the exam.
2. Replace the coordinator with a mutex-guarded visited map — compare the two designs.
3. Add per-host rate limiting (map host → limiter) instead of one global bucket.
4. Stream results to the terminal as they arrive instead of collecting, keeping the summary correct.
5. Add retries with backoff for 5xx/timeouts, max 3 attempts, without exceeding the rate limit.

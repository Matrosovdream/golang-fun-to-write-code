# metricsd — in-memory metrics engine (performance capstone)

A TCP server ingesting metric events at millions/second (`c name 1`, `g name 42`,
`h name 250`), aggregating counters/gauges/histograms in memory, with a JSON query API —
plus the load generator to prove the numbers.

## What this project teaches

- **Sharding**: 64 shards picked by `maphash`, contention ÷ 64. The benchmark pits it
  against a single global mutex and `sync.Map` — and the result teaches the best lesson
  in the repo: on a *stable key set* `sync.Map` wins (~9ns vs ~55ns sharded vs ~128ns
  global mutex on M1 Pro), because its read path skips locks entirely. The bench file
  comment explains why the store still shards — and how to flip the ranking with key
  churn. `go test -bench=BenchmarkStores -benchmem ./internal/store`
- **Read-mostly locking**: RWMutex guards only the *maps*; values are `*atomic.Int64`,
  updated lock-free. First-seen names take the exclusive lock once (double-checked).
- **Zero-allocation hot path**: `Scanner.Bytes()` → borrowed `[]byte` name →
  `map[string(b)]` lookup (compiles to a no-copy probe) → atomic add. The key string is
  allocated exactly once per metric name, ever.
- **Lock-free histograms**: fixed log-scale buckets of atomics; percentile estimates at
  read time — the Prometheus trade explained in 60 lines.
- **Differential fuzzing**: `FuzzParse` compares the fast parser against an obviously
  correct reference (`go test -fuzz=FuzzParse -fuzztime=10s ./internal/parse`).
- **Live profiling**: `net/http/pprof` + `expvar` on the query port. This is the project
  to practice the whole workflow on:

```sh
# with loadgen running:
go tool pprof -top  'http://localhost:9126/debug/pprof/profile?seconds=5'
go tool pprof -http=:9127 'http://localhost:9126/debug/pprof/heap'
curl -s 'http://localhost:9126/debug/pprof/goroutine?debug=1' | head
curl -s localhost:9126/debug/vars | python3 -m json.tool
go tool trace <(curl -s 'http://localhost:9126/debug/pprof/trace?seconds=2')
```

- **GC tuning**: rerun loadgen with `GOGC=200` / `GOMEMLIMIT=128MiB` on the server and
  compare throughput + heap profile.
- **TCP servers**: accept loop, per-conn goroutines, cancellation by closing the
  listener and live conns (there is no ctx-aware Read — this *is* the idiom).

## Run

```sh
go test -race ./...
go test -bench=. -benchmem ./...
go test -fuzz=FuzzParse -fuzztime=10s ./internal/parse

go run ./cmd/metricsd                          # terminal 1
go run ./cmd/loadgen -conns 4 -duration 3s     # terminal 2
curl -s localhost:9126/metrics | python3 -m json.tool | head -30
```

## Rewrite exercises

1. Rewrite the store from the design comment at the top of `store.go`; benchmark yours.
2. Extract the counter/gauge/hist locking dance into one generic `lazyGet[T]` — does the
   benchmark move?
3. Replace per-conn goroutines with a worker pool fed by a channel of batches
   (`sync.Pool` the batches) — measure both under loadgen; which wins and why?
4. Add sliding 60s windows to counters (ring of 60 atomics, ticker rotation).
5. Find the current bottleneck with pprof under loadgen and fix *only* that. Repeat.

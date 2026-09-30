# logparse — log analyzer with a measured optimization story

Parse and aggregate large access logs. The project ships one problem with **2 parsers × 3
processing strategies**, all verified identical in tests — only their speed differs. The point
is the workflow: *measure, change one thing, measure again*.

## What this project teaches

- **Benchmarks first**: `go test -bench=. -benchmem ./internal/parse` — `allocs/op` usually
  explains parser speed before any CPU profile does.
- **Naive vs fast parsing**: one regexp vs a single pass with `strings.Cut` — same result,
  order-of-magnitude difference.
- **Three processing shapes**:
  - `Sequential` — the baseline;
  - `Chunked` — split the file at newline-aligned byte offsets, one lock-free `Stats` per
    worker, merge once (fastest);
  - `Pipeline` — reader fans batches out over a channel, batches recycled via **`sync.Pool`**
    (works on any reader; pays channel overhead — compare!).
- **Preallocation** (`make([]T, 0, n)`), **`strings.Builder`** for the report,
  **`strconv.Append*` into a reused buffer** in `loggen` (allocation-free hot loop),
  **`bufio`** sizing for both reading and writing.
- **Escape analysis**: `go build -gcflags=-m ./internal/parse` — see why `badLine` is a
  separate function.
- **Generics**: `TopN[K comparable]` serves `map[string]int64` and `map[int]int64` alike.
- **pprof on a CLI**: `-cpuprofile` + `go tool pprof`.

## Run

```sh
go test ./...
go test -bench=. -benchmem ./internal/parse

go run ./cmd/loggen -lines 2000000 > /tmp/access.log

go run ./cmd/logparse -file /tmp/access.log -mode sequential -parser naive
go run ./cmd/logparse -file /tmp/access.log -mode sequential
go run ./cmd/logparse -file /tmp/access.log -mode pipeline
go run ./cmd/logparse -file /tmp/access.log -mode chunked

# profile the naive parser and see the regexp engine dominate:
go run ./cmd/logparse -file /tmp/access.log -mode sequential -parser naive -cpuprofile /tmp/cpu.out
go tool pprof -top /tmp/cpu.out | head -15
```

## Reference numbers (M1 Pro, 2M lines / 130MB)

| step | mode | time | rate |
|---|---|---|---|
| baseline | sequential + naive parser | 1.73s | 1.2M lines/s |
| swap the parser | sequential + fast | 391ms | 5.1M lines/s |
| parallel, channels | pipeline + fast | 135ms | 14.8M lines/s |
| parallel, no channels | chunked + fast | 88ms | 22.7M lines/s |

Parser micro-benchmark: naive 757 ns/op (2 allocs/op), fast 61 ns/op (0 allocs/op).
Reproduce all of it with the commands above — your numbers will differ, the *ratios* shouldn't.

## Rewrite exercises

1. Rewrite `ParseFast` from the format spec alone, then benchmark yours vs the original.
2. Make percentiles exact-at-scale: replace the sorted slice with a fixed-bucket histogram.
   What accuracy do you trade? How much memory do you save on 100M lines?
3. Add a `-since`/`-until` time filter — now the timestamp must actually be parsed. Measure
   what `time.Parse` costs per line and cache the layout work.
4. Compare `benchstat` runs of `ParseFast` before/after removing the status range check.
5. Break `chunkBounds` on purpose (drop the newline alignment) and watch the agreement test fail.

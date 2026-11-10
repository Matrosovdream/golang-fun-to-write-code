# Progress

Update this file as projects are completed. See [PLAN.md](PLAN.md) for the full description of each project.

## Current project

🏁 All 35 projects built (15 easy, 10 medium, 10 hard). The second batch (#11–#20, specs in `PLAN.md`) is done and was re-verified independently of its builders. Rewrite time: work through `practice/`.

## Status by project — easy level

| #   | Project | Status | Notes |
|-----|---------|--------|-------|
| E01 | wordfreq — word frequency counter | done | slices, maps, sort.Slice, bufio, io.Reader, golden tests |
| E02 | tempconv — unit converter | done | named types, methods, Stringer, iota, strconv |
| E03 | passgen — password generator | done | crypto/rand vs math/rand, strings.Builder, go:embed |
| E04 | guessgame — guessing game | done | stdin loop, input validation, score file, exit codes |
| E05 | dirsize — disk usage tool | done | filepath.WalkDir, io/fs, skip-and-report errors, byte formatting |
| E06 | grepish — mini grep | done | regexp, flags, stderr vs stdout, exit codes as API |
| E07 | csvreport — CSV aggregator | done | encoding/csv, time.Parse, tabwriter, row-numbered errors |
| E08 | jsontool — JSON inspector | done | map[string]any, type switches/assertions, json.Indent |
| E09 | todojson — minimal todo | done | JSON persistence, hand-rolled subcommands (pre-cobra) |
| E10 | logmini — own logging package | done | io.Writer, multi-package module, embedding, buffer tests |
| E11 | stopwatch — timers | done | Duration, Ticker vs Timer, first select, signal.Notify |
| E12 | quizcli — timed quiz | done | first goroutine, channels, select + time.After, -race |
| E13 | fetchcli — tiny curl | done | http.Client with timeout, io.Copy, body closing |
| E14 | pageserve — first web server | done | HandleFunc, FileServer, html/template, mutex counter, httptest |
| E15 | multidl — concurrent downloader | done | WaitGroup, result channel, semaphore, errors.Join |

## Status by project — medium & hard

| #  | Project | Level | Status | Notes |
|----|---------|-------|--------|-------|
| 01 | taskcli — task manager CLI | medium | done | cobra, viper, SQLite, repository pattern, functional options |
| 02 | shortlink — URL shortener API | medium | done | net/http, chi, middleware, slog, graceful shutdown, httptest |
| 03 | fetchpool — concurrent link checker | medium | done | worker pool, pipelines, fan-in/out, errgroup, rate limiting, -race |
| 04 | weathercache — API client + cache | medium | done | http.Client, retries/backoff, LRU+TTL cache, decorator, mockery |
| 05 | logparse — log analyzer + benchmarks | medium | done | bufio, generics, pprof, sync.Pool, benchstat, escape analysis |
| 06 | gobank — banking REST API | hard | done | pgx/sqlx, Postgres, migrations, JWT, zap, testcontainers, Docker |
| 07 | chathub — WebSocket chat | hard | done | gorilla/websocket, hub pattern, presence, graceful drain |
| 08 | grpc-inventory — gRPC services | hard | done | protobuf, all 4 RPC kinds, interceptors, deadlines, bufconn |
| 09 | jobqueue — background jobs | hard | done | go-redis, retries, DLQ, delayed jobs, state machine, miniredis |
| 10 | metricsd — metrics engine | hard | done | TCP ingest, sharding, atomics, full pprof workflow, fuzzing |
| 11 | dagrun — task graph runner | medium | done | topological sort, cycle paths, os/exec, process-group cancellation, fake executor |
| 12 | searchlite — full-text search | medium | done | inverted index, BM25, container/heap, query parser, atomic snapshot |
| 13 | lbproxy — reverse proxy / load balancer | medium | done | ReverseProxy, 4 strategies, health checks, consistent hash, atomic.Pointer reload |
| 14 | sitegen — static site generator | medium | done | io/fs + MapFS, goldmark, html/template, fsnotify debounce, SSE live reload |
| 15 | obsapi — observability | medium | done | Prometheus RED metrics, OpenTelemetry traces, trace-correlated slog, cardinality |
| 16 | tinylang — scripting language | hard | done | Pratt parser, tree-walker + bytecode VM, closures, differential tests, fuzzing |
| 17 | lsmkv — LSM storage engine | hard | done | WAL, skip list, SSTables, bloom filter, compaction, crash + model-based tests |
| 18 | crdtpad — collaborative editing | hard | done | RGA CRDT, Lamport clocks, causal buffering, WebSocket sync, convergence property tests |
| 19 | orderflow — outbox + saga pipeline | hard | done | embedded NATS JetStream, outbox, inbox idempotency, saga compensation, DLQ, chaos tests |
| 20 | raftkv — Raft key-value store | hard | done | election, log replication, persistence, simulated network faults, safety invariants |

## Log

- 2026-11-10 — 🏁 Second batch complete: #11–#20 built and verified (gofmt/vet/`-race` rerun, every README demo run, one hand-made mutation per project confirmed to turn a test red). Medium: dagrun, searchlite, lbproxy, sitegen, obsapi. Hard: tinylang, lsmkv, crdtpad, orderflow, raftkv. Fix found in verification: orderflow ignored SIGTERM (ungraceful exit, leaked temp dir) — now handled like every other service. **Size caveat:** most projects overshoot the retype targets (medium 400–700 lines, hard 600–1,200): sitegen 1,098, searchlite 1,047, lbproxy 1,041, tinylang 1,795 (guard was ~1,300), lsmkv 1,457, raftkv 1,437, orderflow 1,377, crdtpad 1,138; dagrun 678 and obsapi 695 are within target. Each README says what is inherent. Not verified anywhere: Linux/Windows, Jaeger half of obsapi's compose file (image unreachable).
- 2026-10-01 — 🏁 Easy level complete: E13–E15 built (fetchcli: http.Client discipline, streaming, redirects; pageserve: first server, html/template XSS test, mutex counter, fstest.MapFS; multidl: WaitGroup + semaphore + closer goroutine, errors.Join — plus a real filename-collision bug found in the demo and fixed with a test). All 25 projects done.

- 2026-10-01 — ✅ E10–E12 built: logmini (io.Writer destination, shared sink, embedding, package default), stopwatch (Ticker vs Timer, first select, \r repaint), quizcli (goroutine+channel+select deadline on input, three tested endings). Race-tested, demos run.

- 2026-10-01 — ✅ E07–E09 built: csvreport (encoding/csv, row-numbered errors, time.Parse), jsontool (dynamic JSON, type switches, json.Number precision test), todojson (JSON persistence, hand-rolled subcommands, slices.IndexFunc/DeleteFunc). Tests green, demos run.

- 2026-10-01 — ✅ E01–E06 built: wordfreq (maps/sort/golden tests), tempconv (named types, Stringer, errors.Is), passgen (crypto/rand, go:embed, property tests), guessgame (stdin loop, testable play()), dirsize (WalkDir, skip-and-report policy), grepish (regexp, exit codes as API). All tests green, demos run.

- 2026-10-01 — 📋 Easy level planned: 15 fundamentals projects (E01–E15) added to `PLAN.md`, stdlib-first, ordered as a progression up to the medium level.
- 2026-09-30 — 🎉 Repo started. Plan for 10 projects (5 medium, 5 hard) written in `PLAN.md`.
- 2026-09-30 — 🏁 All 10 projects complete. Every project: tests green (concurrent ones with -race), live demo run and verified.
- 2026-09-30 — ✅ 10 metricsd: 26M events/s measured ingest, zero-alloc parser (19ns/op), sharded store benchmarked vs global mutex vs sync.Map (surprise: sync.Map wins on stable keys — see bench comment), lock-free histograms, live pprof under load, differential fuzzing that found a real bug ("c \r 0") now kept as a regression seed. Hard level complete! 🎉
- 2026-09-30 — ✅ 09 jobqueue: reliable queue (LMOVE + leases), reaper chaos test, delayed jobs & backoff via one zset, explicit state machine, panic recovery as failed attempt, graceful drain with context.WithoutCancel, go-redis + miniredis + robfig/cron. Demo: flaky→done in 3 attempts, panic→DLQ, delayed promoted.
- 2026-09-30 — ✅ 08 grpc-inventory: buf codegen (no system protoc), all 4 RPC shapes, interceptors (logging+recovery, unary+stream), metadata, deadline propagation, hand-rolled circuit breaker, health+reflection, bufconn tests. Full 2-service demo verified.
- 2026-09-30 — ✅ 07 chathub: hub pattern (all state in one goroutine, zero mutexes), read/write pumps, ping/pong deadlines, slow-consumer drop policy, history ring buffer, graceful drain with close frames. Race-tested ×3, live 2-client demo OK.
- 2026-09-30 — ✅ 06 gobank: clean architecture, unit-of-work TxManager over context, SELECT FOR UPDATE with ordered locking (concurrency-tested), sqlx+pgx, embedded golang-migrate, bcrypt+JWT, zap, viper, testcontainers integration suite, multi-stage Dockerfile. Ports: API :8085, Postgres :5435 (5433/5434/8081 taken by other local projects). Live demo verified.
- 2026-09-30 — ✅ 05 logparse: 2 parsers × 3 strategies, all agreement-tested. Measured on 2M lines: naive 1.73s → fast 391ms → pipeline(sync.Pool) 135ms → chunked 88ms (~20x). Benchmarks, preallocation, strings.Builder, strconv.Append, escape analysis, pprof. Medium level complete! 🎉
- 2026-09-30 — ✅ 04 weathercache: decorator at two layers (Provider cache + RoundTripper retry), generic LRU+TTL with injectable clock, httptest fake API and testify/mock side by side. Demo: 2s API vs 1µs cache hit.
- 2026-09-30 — ✅ 03 fetchpool: worker pool + single-goroutine coordinator (nil-channel trick), errgroup.SetLimit, x/time/rate, atomic counters, ctx cancellation end-to-end, x/net/html. `go test -race` green, live demo OK.
- 2026-09-30 — ✅ 02 shortlink: chi + middleware chain, slog, handler→service→repo, central error mapping, graceful shutdown, httptest black-box tests. Live demo verified (302 + hit counting + validation).
- 2026-09-30 — ✅ 01 taskcli: cobra+viper CLI, repository pattern over SQLite & JSON, functional options, sentinel errors, conformance test suite. Tests green, demo run OK.

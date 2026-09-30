# Progress

Update this file as projects are completed. See [PLAN.md](PLAN.md) for the full description of each project.

## Current project

🏁 All 10 projects built. Now it's rewrite time: pick a project, build your copy in `practice/`, keep its tests green.

## Status by project

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

## Log

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

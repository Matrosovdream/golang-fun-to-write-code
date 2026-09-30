# Plan — Go projects from medium to hard

The goal of this repo: I (Claude) build each project with idiomatic Go, best practices, popular
libraries and optimization techniques. You then re-type/rewrite the code yourself, run it, break it,
and fill the gaps in your Go knowledge. Comments appear only where something non-obvious happens.

Structure: `projects/{level}/{projectname}`. Two levels, 5 projects each.
Every project is self-contained: its own `go.mod`, its own `README.md` (what it teaches, how to run,
suggested exercises).

---

## Coverage map

What the 10 projects cover in total, so you can see nothing important is missed:

| Area | Where it's covered |
|---|---|
| Structs, interfaces, embedding, generics | all; generics deep-dive in `logparse` |
| Error handling: wrapping, `errors.Is/As`, sentinel errors, custom types | `taskcli`, `shortlink`, everywhere after |
| Goroutines, channels, `select`, worker pools, fan-in/fan-out, pipelines | `fetchpool` (dedicated), `chathub`, `jobqueue` |
| `context`: cancellation, timeouts, values | `fetchpool`, `shortlink`, all hard projects |
| `sync`: Mutex, RWMutex, WaitGroup, Once, `sync.Map`, atomic, errgroup, semaphore | `fetchpool`, `weathercache`, `metricsd` |
| Design patterns: repository, functional options, decorator, strategy, observer, factory, builder, middleware, state machine | spread across all; called out per project below |
| Clean architecture (handler → service → repo), dependency injection | `shortlink` (light), `gobank` (full) |
| HTTP servers: `net/http`, chi, middleware, graceful shutdown | `shortlink`, `gobank` |
| HTTP clients: retries, backoff, timeouts | `weathercache` |
| Databases: `database/sql`, sqlx/pgx, PostgreSQL, SQLite, migrations, transactions | `taskcli` (SQLite), `gobank` (Postgres) |
| CLI apps: cobra, viper, flags | `taskcli` |
| Config: env vars, files, viper / envconfig | `taskcli`, `gobank` |
| Logging: `log/slog` (stdlib), zap | `shortlink` (slog), `gobank` (zap) |
| Testing: table-driven, testify, mocks (mockery), `httptest`, integration tests, testcontainers, race detector, fuzzing | grows project by project; full setup in `gobank` |
| gRPC: protobuf, unary + streaming, interceptors, deadlines | `grpc-inventory` |
| WebSockets, pub/sub, hub pattern | `chathub` |
| Background jobs, retries, dead-letter queues, Redis | `jobqueue` |
| JWT auth, password hashing, validation | `gobank` |
| Optimization: benchmarks, pprof, `sync.Pool`, preallocation, escape analysis, `strings.Builder`, buffered I/O, sharding, GC tuning | `logparse` (intro), `metricsd` (deep dive) |
| Docker, docker-compose, Makefile | hard level |

---

## Medium level — `projects/medium/`

Order matters: each project assumes the previous ones.

### 1. `taskcli` — task manager CLI
A todo/task manager in the terminal: add, list, complete, delete tasks; storage in SQLite with a
JSON-file fallback.

- **Patterns:** repository (one interface, two implementations), functional options, factory.
- **Stdlib:** `encoding/json`, `os`, `time`, `errors` (wrapping, `Is`/`As`, sentinel errors), `text/tabwriter`.
- **Libraries:** `spf13/cobra` (commands), `spf13/viper` (config), `mattn/go-sqlite3` or `modernc.org/sqlite` via `database/sql`.
- **Testing:** table-driven tests, `testify/require`, testing against a temp SQLite file.
- **Teaches:** project layout (`cmd/`, `internal/`), how interfaces decouple storage, idiomatic error flow.

### 2. `shortlink` — URL shortener REST API
An HTTP service: `POST /shorten` → short code, `GET /{code}` → redirect, `GET /stats/{code}` → hits.

- **Patterns:** middleware chain (logging, request ID, recovery), handler → service → repository, dependency injection by hand (constructors), decorator.
- **Stdlib:** `net/http` (mux improvements of 1.22+), `context`, `log/slog`, graceful shutdown with `signal.NotifyContext`.
- **Libraries:** `go-chi/chi` (router + middleware), `go-playground/validator`.
- **Testing:** `httptest`, mocking the repository via an interface.
- **Teaches:** how a Go HTTP service is actually structured; the request lifecycle; context propagation.

### 3. `fetchpool` — concurrent link checker / crawler
Feed it URLs (or a start page), it crawls concurrently, reports dead links and timings.

- **Patterns:** worker pool, pipeline (generator → workers → collector), fan-in/fan-out, bounded concurrency with a semaphore channel, rate limiting.
- **Stdlib:** goroutines, channels, `select`, `context` cancellation, `sync.WaitGroup`, `sync/atomic` counters, `time.Ticker`.
- **Libraries:** `golang.org/x/sync/errgroup`, `golang.org/x/time/rate`.
- **Testing:** race detector (`go test -race`), fake HTTP servers with `httptest`.
- **Teaches:** the entire core concurrency toolbox in one runnable place. The most important medium project.

### 4. `weathercache` — API client with a caching layer
A CLI/service that calls a public weather API (Open-Meteo, no key needed), with a TTL cache in front
and resilient HTTP client behavior.

- **Patterns:** decorator (cached client wraps real client behind one interface), strategy (cache eviction), adapter.
- **Stdlib:** `http.Client` done right (timeouts, transport reuse), `sync.RWMutex` cache, `container/list` LRU, `time` TTLs.
- **Libraries:** retries with exponential backoff + jitter (hand-written, then `cenkalti/backoff`), `vektra/mockery` for generated mocks.
- **Testing:** mocking HTTP with `httptest.Server` and with generated interface mocks — both approaches compared.
- **Teaches:** consuming external APIs in production style; why everything hides behind an interface.

### 5. `logparse` — log analyzer with benchmarks
Parse large (multi-GB capable) access-log files, aggregate stats (top IPs, status codes, latencies),
then optimize it measurably.

- **Patterns:** iterator, generics for aggregation (`Top[T]`, generic counters), options.
- **Stdlib:** `bufio.Scanner` vs `bufio.Reader`, `strings` vs `regexp` parsing, `sort`/`slices`, `flag`.
- **Optimization (the point of this project):** `go test -bench`, `benchstat`, pprof (CPU + heap), `sync.Pool`, slice preallocation, avoiding allocations in hot loops, `strings.Builder`, escape analysis (`-gcflags=-m`), parallel processing of file chunks.
- **Teaches:** measure-first optimization workflow; you'll make the parser ~10x faster step by step, with each step kept in the README.

---

## Hard level — `projects/hard/`

These use Docker for infrastructure (Postgres, Redis) via per-project `docker-compose.yml`.
Where possible there is a no-Docker fallback (SQLite, `miniredis`) so everything stays runnable.

### 1. `gobank` — banking REST API (the production-grade reference)
Accounts, transfers between them, transaction history. The "how a real Go backend looks" project.

- **Patterns:** clean architecture (transport / service / repository / domain), unit-of-work for DB transactions, DTOs, repository, middleware.
- **Libraries:** `go-chi/chi`, `jackc/pgx` + `sqlx` (raw SQL, no ORM — and a note on when GORM is fine), `golang-migrate` migrations, `uber-go/zap`, `spf13/viper`, `go-playground/validator`, `golang-jwt/jwt` auth, `bcrypt` password hashing.
- **Infra:** PostgreSQL in docker-compose, Makefile targets (`make run`, `make test`, `make migrate`), multi-stage Dockerfile.
- **Testing:** unit tests with mocks, integration tests with `testcontainers-go`, coverage.
- **Teaches:** SQL transactions and isolation (concurrent transfers without lost updates — deadlock handling), auth flow, config per environment, project layout at scale.

### 2. `chathub` — real-time WebSocket chat
Multi-room chat server + tiny TUI client. Nicknames, rooms, presence ("user joined/left"), message history ring buffer.

- **Patterns:** hub/broker (the canonical Go channels architecture), observer (subscriptions), command pattern for client messages.
- **Libraries:** `gorilla/websocket` (or `coder/websocket` — README compares), `google/uuid`.
- **Concurrency:** one goroutine per connection read/write pump, unbuffered vs buffered channel decisions, slow-consumer handling (drop vs disconnect), graceful shutdown that drains connections.
- **Testing:** concurrent integration tests with real WebSocket clients, `-race` as a hard requirement.
- **Teaches:** long-lived connection management — where naive channel code deadlocks and how the hub pattern avoids it.

### 3. `grpc-inventory` — gRPC microservices pair
Two services: `inventory` (products, stock) and `orders` (places orders, calls inventory over gRPC). Plus a small CLI client.

- **Patterns:** API-first design with protobuf contracts, interceptor (gRPC middleware), circuit-breaker (simple hand-rolled).
- **Libraries:** `protobuf` + `protoc-gen-go`, `google.golang.org/grpc`, `buf` for proto management (optional), `grpc-ecosystem` interceptors for logging/recovery.
- **gRPC features covered:** unary, server-streaming (stock watch), client-streaming (bulk upload), bidirectional; deadlines/timeouts, metadata, error codes with `status`, health checks, graceful stop.
- **Testing:** in-memory gRPC with `bufconn` — no network needed in tests.
- **Teaches:** service-to-service communication, codegen workflow, why deadlines must propagate.

### 4. `jobqueue` — background job processing system
A producer API + worker fleet: enqueue jobs (e.g. "send email", "resize image"), workers execute with retries, exponential backoff, dead-letter queue, scheduled/delayed jobs, priorities.

- **Patterns:** state machine (job lifecycle: pending → running → done/failed/dead), producer-consumer, dispatcher, heartbeats.
- **Libraries:** `redis/go-redis` (queues via lists/sorted-sets), `alicebob/miniredis` (tests + no-Docker fallback), `robfig/cron` for scheduled jobs.
- **Concurrency:** worker pool with per-worker graceful drain, `context` deadline per job, panic recovery in workers, at-least-once semantics and idempotency.
- **Testing:** miniredis-backed tests, chaos test (kill workers mid-job, verify redelivery).
- **Teaches:** everything async: how Sidekiq/Celery-style systems work inside, built from scratch.

### 5. `metricsd` — in-memory metrics/time-series engine (performance deep dive)
A server that ingests high-rate metrics (`name value timestamp`) over TCP/HTTP, aggregates in memory
(counters, gauges, histograms with windows), and answers queries. Then: profile and optimize under load,
with a bundled load generator.

- **Patterns:** sharding (N maps + N mutexes vs `sync.Map` — benchmarked against each other), ring buffers, object pooling, lock-free counters with `atomic`.
- **Optimization (the point):** full pprof workflow (CPU, heap, goroutine, mutex, block profiles; flame graphs), `sync.Pool` for parse buffers, zero-allocation parsing (`[]byte`, no `strings.Split` in hot path), `GOGC`/`GOMEMLIMIT` tuning, `GOMAXPROCS`, benchmark-driven development, `pprof` over HTTP on a live server, execution tracer (`go tool trace`).
- **Stdlib:** `net` (raw TCP), `bufio`, `encoding/binary`, `expvar`.
- **Testing:** benchmarks as first-class tests, fuzzing the parser (`go test -fuzz`), load test comparing before/after numbers in the README.
- **Teaches:** how to find and remove bottlenecks with proof, not guesses. The capstone.

---

## Working process (per project)

1. I build the project fully, with its README (goals, run instructions, exercise ideas for your rewrite).
2. I run it and show the result.
3. Status in `PROGRESS.md` → `done`, log entry added.
4. You rewrite it in your own `practice/` copy (or from scratch), run it, compare.
5. We move to the next one.

Comments in code follow the repo rule: only where the *why* isn't obvious — no narration.

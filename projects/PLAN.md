# Plan — Go projects from easy to hard

The goal of this repo: I (Claude) build each project with idiomatic Go, best practices, popular
libraries and optimization techniques. You then re-type/rewrite the code yourself, run it, break it,
and fill the gaps in your Go knowledge. Comments appear only where something non-obvious happens.

Structure: `projects/{level}/{projectname}`. Three levels: **easy** (15 small projects,
fundamentals, almost pure stdlib), **medium** (10), **hard** (10). Medium and hard were extended
from 5 to 10 each after the first pass; the second five of each fill the gaps the coverage map
below exposed (graphs and subprocesses, search, proxies, templates/watchers, observability,
interpreters, storage engines, CRDTs, messaging, consensus).
Every project is self-contained: its own `go.mod`, its own `README.md` (what it teaches, how to run,
suggested exercises).

The easy level was added after medium+hard were built: it back-fills the fundamentals those
projects assume, so every language feature gets its own small, rewritable home first.

---

## Coverage map

What the medium+hard projects cover in total, so you can see nothing important is missed.
The easy level (described below) back-fills the fundamentals underneath this table: core
syntax, slices/maps, methods and named types, `fmt.Stringer`, `io.Reader`/`io.Writer`,
`strconv`, `regexp`, `encoding/csv`, dynamic JSON, `filepath.WalkDir`, `go:embed`,
`html/template`, `time`/`select`, terminal I/O, exit codes, and the first goroutines,
channels and WaitGroup.

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
| Docker, docker-compose, Makefile | hard level (`gobank`, `jobqueue`), optional in `obsapi` |
| Graph algorithms, topological sort, `os/exec`, subprocess cancellation | `dagrun` |
| Search: inverted index, BM25, `container/heap`, recursive-descent query parsing, atomic file writes | `searchlite` |
| Reverse proxy, load balancing, consistent hashing, health checks, hot reload (`atomic.Pointer`) | `lbproxy` |
| `io/fs`, `embed`, `html/template` layouts, markdown, file watching + debounce, Server-Sent Events | `sitegen` |
| Observability: Prometheus metrics, OpenTelemetry traces, trace-correlated `slog`, health/readiness | `obsapi` |
| Interpreters: lexer, Pratt parser, tree-walking evaluator, bytecode compiler + VM, closures, differential testing | `tinylang` |
| Storage engines: WAL, fsync, SSTables, bloom filters, compaction, crash recovery, model-based testing | `lsmkv` |
| Eventual consistency: CRDTs, Lamport clocks, idempotent/commutative ops, randomized-interleaving tests | `crdtpad` |
| Messaging: NATS JetStream, transactional outbox, idempotent consumers, sagas, dead-letter subjects | `orderflow` |
| Distributed consensus: Raft, simulated networks with fault injection, injectable clocks | `raftkv` |

---

## Easy level — `projects/easy/`

Rules of the level: each project is 50–250 lines, rewritable in one sitting, stdlib-only
unless noted, and owns one cluster of fundamentals. Order is a progression — by E15 you've
quietly met everything `fetchpool` and `shortlink` take for granted.

### E01. `wordfreq` — word frequency counter
Read text (file or stdin), count words, print the top N with counts.

- **Topics:** slices, maps, `strings` (Fields/ToLower/Trim), `sort.Slice`, `bufio.Scanner`, `flag`, reading stdin vs files (`io.Reader` as the unifying type).
- **Tests:** first table-driven test; golden output comparison.

### E02. `tempconv` — unit converter
`tempconv 100C` → `212°F / 373.15K`; also length and weight.

- **Topics:** named types (`type Celsius float64`) and methods, value receivers, the `fmt.Stringer` interface, constants and iota, switch, parsing with `strconv`.
- **Tests:** table-driven conversions, including edge cases (absolute zero validation → first custom error).

### E03. `passgen` — password & passphrase generator
Flags for length, count, charset classes, or diceware-style word phrases.

- **Topics:** `crypto/rand` vs `math/rand/v2` (and why it matters here), `strings.Builder`, byte vs rune, embedding a wordlist with `go:embed`, flags.
- **Tests:** property-style checks (length, charset membership, uniqueness across calls).

### E04. `guessgame` — number guessing game
Interactive: the program picks 1–100, you guess, it says higher/lower; tracks attempts and best score in a file.

- **Topics:** stdin interaction loop (`bufio.Reader`), input validation + re-prompting, `math/rand/v2`, writing/reading a tiny score file, `os.Exit` codes, labeled break.

### E05. `dirsize` — disk usage tool
`dirsize ~/Downloads` → total size + top 10 biggest files/dirs, human-readable sizes.

- **Topics:** `filepath.WalkDir` (and when you'd recurse by hand), `io/fs` interfaces, `os.FileInfo`, error handling policy for unreadable dirs (skip & report, don't die), sorting, formatting bytes (KB/MB/GB helper).

### E06. `grepish` — mini grep
`grepish -i -n "pattern" file...` with `-r` for directories.

- **Topics:** `regexp` (compile once!), `bufio` line scanning, combining flags, `filepath.WalkDir` again, exit codes as API (0 match / 1 none / 2 error, like real grep), writing to stderr vs stdout.

### E07. `csvreport` — sales CSV aggregator
Read a sales CSV (date, region, product, amount), aggregate by region/month, write a summary table (text + CSV out).

- **Topics:** `encoding/csv`, struct-per-row parsing, `time.Parse` layouts, map-of-struct aggregation, `text/tabwriter`, error wrapping with row numbers (`row 17: %w`).

### E08. `jsontool` — JSON pretty-printer & path extractor
`cat data.json | jsontool -pretty` and `jsontool -path users.3.name file.json`.

- **Topics:** `encoding/json` without target structs (`map[string]any`, `[]any`), type switches and type assertions, stdin pipes, `json.Indent`, `json.Number` vs float64 gotcha.

### E09. `todojson` — minimal todo list
`todojson add "buy milk"`, `list`, `done 2` — one JSON file, no external deps.

- **Topics:** structs + JSON tags, marshal/unmarshal round trip, hand-rolled subcommands (`os.Args[1]` switch — see what cobra automates before using it), `time.Time` in JSON, slices.IndexFunc / slices.DeleteFunc.
- This is `taskcli`'s little sibling: rewrite this, then compare with the cobra version.

### E10. `logmini` — write your own logging package
A reusable `logmini` package (levels, prefixes, timestamps, any destination) + a demo app using it.

- **Topics:** your first multi-package module, exported vs unexported, **`io.Writer` as the destination** (the single most important interface in Go), variadic functions, struct embedding for a `DefaultLogger`, package-level default instance (how `log` itself does it).
- **Tests:** log into a `bytes.Buffer` and assert — the io.Writer payoff.

### E11. `stopwatch` — stopwatch & pomodoro timer
`stopwatch` (laps on Enter) and `stopwatch -pomodoro 25m` with a live countdown.

- **Topics:** the `time` package properly (Duration arithmetic, `Ticker` vs `Timer`, monotonic clocks), first `select`, `signal.Notify` for clean Ctrl+C, carriage-return terminal updates.

### E12. `quizcli` — timed quiz game
Questions from CSV, total time limit; unanswered questions count as wrong when time runs out.

- **Topics:** first real goroutine (reading answers while the clock runs), channels, `select` between answer channel and `time.After` — the classic concurrency on-ramp. `-race` from here on.

### E13. `fetchcli` — tiny curl
`fetchcli -o page.html -timeout 5s https://go.dev` — status, headers, body/file output, redirects shown.

- **Topics:** `net/http` client basics done right (never `http.Get` without a timeout), `io.Copy` (streams, not `ReadAll`), `defer resp.Body.Close()` and why, `os.Create`, inspecting `resp.Header`.

### E14. `pageserve` — first web server
Serves a directory listing page + static files; a `/stats` page shows request counts.

- **Topics:** `net/http` server basics (`HandleFunc`, `http.FileServer`, `http.StripPrefix`), **`html/template`** (and why not `text/template` for HTML), a mutex-guarded counter — first shared state under concurrency, `httptest` for handlers.

### E15. `multidl` — concurrent downloader
`multidl urls.txt` downloads every URL concurrently to files, with a live progress line and a summary.

- **Topics:** goroutines + `sync.WaitGroup`, results over a channel, limiting concurrency with a buffered-channel semaphore, `errors.Join` for the summary.
- The bridge project: after this, `fetchpool` (medium) is the same shape with the full toolbox.

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

Projects 6–10 below are the second batch (overall numbers #11–#15 in `PROGRESS.md`). Same rule:
each assumes the ones before it. Size target for every project: 400–700 source lines plus tests,
so it stays rewritable in a sitting or two.

### 6. `dagrun` — task graph runner (a tiny `make` / CI engine) — overall #11
`dagrun -f dagrun.yaml -j 4 build test` reads a YAML task file (`cmd`, `deps`, `env`, `dir` per task),
runs independent tasks in parallel, streams prefixed output (`[build] …`), skips the dependents of a
failed task (`-k` keeps going), and kills whole child process trees on Ctrl+C. `--dry-run` prints the
plan level by level; `--dot` emits Graphviz.

- **Patterns:** strategy (an `Executor` interface — real `os/exec` vs a fake in tests), observer (scheduler emits events, a renderer consumes them), indegree-counting scheduler over a ready channel.
- **Algorithms:** Kahn's topological sort, cycle detection that reports the cycle path (`a → b → c → a`), transitive dependency closure for "run only what `deploy` needs".
- **Stdlib:** `os/exec` (`CommandContext`, `Cmd.Cancel`, `WaitDelay`, process groups via `SysProcAttr` behind a `//go:build unix` file), a line-prefixing `io.Writer` that never interleaves mid-line, `context`, `sync`, `slices`/`maps`, `flag`.
- **Libraries:** `gopkg.in/yaml.v3`.
- **Testing:** fake executor with scripted latency/failure; assert no task starts before its deps finish, the `-j` limit is never exceeded (atomic high-water mark), failure skips exactly the transitive dependents, cancellation stops everything, cycle errors name the path; `-race`.
- **Teaches:** graph algorithms in real Go, subprocess management, ctx cancellation reaching child processes, concurrent output without garbling.

### 7. `searchlite` — full-text search engine — overall #12
`searchlite index ./docs`, `searchlite query 'goroutine AND "memory model" -mutex'`, `searchlite serve` —
index text/markdown files (an embedded demo corpus ships with it), query from the CLI or over HTTP,
ranked results with highlighted snippets.

- **Patterns:** pipeline (analyzer = tokenizer → lowercase → stopwords → light stemmer, each stage a small func so stages compose), iterator, functional options.
- **Core:** positional inverted index (term → postings with doc, term frequency, positions), BM25 scoring, top-K via `container/heap` (O(n log k)), merge-intersect / merge-union over sorted postings, phrase queries via positions, recursive-descent query parser (`AND`/`OR`/`-NOT`, `"phrases"`, parentheses) with positioned parse errors.
- **Stdlib:** `container/heap`, generics (`TopK[T]`), `encoding/gob` snapshot persisted with write-temp-then-`os.Rename`, `sync.RWMutex` for live indexing, `net/http` 1.22 mux patterns (`GET /search`, `POST /docs`), `embed`, `unicode`.
- **Libraries:** none beyond stdlib — the point is the data structures.
- **Testing:** table-driven analyzer and parser tests, ranking sanity on a known corpus, phrase vs non-phrase cases, snapshot round trip, a property test checking merge-intersect/union against naive map-based references, benchmarks for indexing and querying.
- **Teaches:** how search actually works; sorted-slice merge algorithms; heaps; recursive-descent parsing; crash-safe file replacement.

### 8. `lbproxy` — L7 reverse proxy & load balancer — overall #13
`lbproxy -config lb.yaml`: routes by host/path prefix to backend pools; strategies round-robin, smooth
weighted round-robin, least-connections and consistent-hash (ring with virtual nodes); active health
checks with rise/fall thresholds; passive ejection after consecutive failures; retries of idempotent
requests on another backend; config hot-reload on `SIGHUP` without dropping in-flight requests;
`/lb/status` JSON + `expvar`.

- **Patterns:** strategy (`Balancer` interface), decorator (middleware around the proxy), copy-on-write config behind `atomic.Pointer[Config]`, state machine (backend health with hysteresis).
- **Stdlib:** `net/http/httputil.ReverseProxy` (`Rewrite`, `ErrorHandler`, `ModifyResponse`), `http.Transport` tuning (idle conns, timeouts), `atomic.Pointer`, `hash/crc32`, `sort.Search` over the ring, `expvar`, `signal.Notify`, graceful shutdown.
- **Libraries:** `gopkg.in/yaml.v3` for config.
- **Testing:** `httptest.Server` backends (slow, failing, flapping); assert even round-robin, weighted proportions, hash stickiness and *minimal remap* when a node leaves, health transitions with an injected ticker/clock, no retry for non-idempotent methods, reload under concurrent load with `-race`.
- **Teaches:** how nginx/envoy-style proxies work; `ReverseProxy` internals; hot reload by atomic swap; consistent hashing.

### 9. `sitegen` — static site generator with a live-reload dev server — overall #14
`sitegen build ./site -o ./public` and `sitegen serve ./site`: Markdown + YAML front matter
(`title`, `date`, `tags`, `draft`), layouts in `html/template` (embedded default theme, overridable),
index and per-tag pages, RSS (`encoding/xml`) and `sitemap.xml`; the dev server watches files,
rebuilds incrementally, and refreshes the browser.

- **Patterns:** pipeline (discover → parse → render → write), debouncer for event storms, functional options, content-hash incremental build cache.
- **Stdlib:** `io/fs` (the generator takes an `fs.FS`, so tests use `fstest.MapFS`), `embed`, `html/template` (funcs, `block`/`define` layouts), `encoding/xml`, `crypto/sha256`, `net/http` Server-Sent Events via `http.Flusher`, a worker pool rendering pages in parallel.
- **Libraries:** `yuin/goldmark` (+ GFM extension), `fsnotify/fsnotify`, `gopkg.in/yaml.v3`.
- **Testing:** build from `fstest.MapFS`; golden HTML for one page; front-matter edge cases; drafts excluded; RSS unmarshals back and validates; debouncer with a fake timer; SSE handler via `httptest`; unchanged files are *not* re-rendered on the second build.
- **Teaches:** `io/fs` as the abstraction that makes file code testable, templates done properly, why file watchers fire five events per save, SSE.

### 10. `obsapi` — observability for a Go service: metrics, traces, correlated logs — overall #15
Two tiny HTTP services (`gateway` → `catalog`) instrumented the way production services are.
Optional `docker-compose.yml` adds Prometheus + Jaeger; without Docker, spans print to stdout and
`/metrics` is scraped with curl.

- **Patterns:** middleware (RED metrics + tracing), decorator (instrumented repository), context as the carrier of trace identity, provider/exporter separation.
- **Metrics:** `prometheus/client_golang` — request counter, latency histogram with chosen buckets, in-flight gauge, a custom collector; labels use the route *pattern* never the raw path (cardinality, with a test that proves it).
- **Tracing:** OpenTelemetry (`otel`, `otelhttp`, SDK) — handler → service → repo spans, errors and events on spans, W3C `traceparent` propagation across the two services, sampler choice; exporter swappable (stdout / OTLP).
- **Logging:** a `slog.Handler` wrapper that stamps `trace_id`/`span_id` from the context so a log line leads to its trace.
- **Operations:** `/healthz` vs `/readyz` (readiness flips during shutdown drain), tracer-provider flush on graceful shutdown.
- **Testing:** SDK `tracetest` span recorder — gateway and catalog spans share one trace ID with correct parent/child; `prometheus/testutil` assertions on counters; random-path 404s don't mint new label values; slog output contains the trace ID.
- **Teaches:** the three pillars wired together; why context is the transport for observability; cardinality as the production footgun.

---

## Hard level — `projects/hard/`

Projects 1–5 use Docker for infrastructure (Postgres, Redis) via per-project `docker-compose.yml`,
with a no-Docker fallback (SQLite, `miniredis`) where possible so everything stays runnable.
Projects 6–10 (the second batch, overall #16–#20) need only the Go toolchain — `orderflow` even
embeds its own NATS server. Size target: 600–1,200 source lines plus tests; where a project is
bigger in concept (`lsmkv`, `raftkv`), the README names what was deliberately left out.

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
- **Teaches:** how to find and remove bottlenecks with proof, not guesses. The capstone of the first batch.

### 6. `tinylang` — a scripting language: lexer, parser, evaluator, bytecode VM — overall #16
`tinylang run prog.tl`, a REPL, and `-engine=eval|vm`. Integers, booleans, strings, arrays,
`let`, `if/else`, `while`, first-class functions and closures, builtins (`len`, `push`, `print`, …),
errors reported as `line:col`. Two execution engines for the same language, benchmarked head to head.

- **Patterns:** interpreter, AST as a sum type (interface + marker method, exhaustive type switches), Pratt parsing (prefix/infix function tables, binding powers), environment chain for closures, constant pool + stack VM with call frames and captured free variables.
- **Optimization:** `fib(30)` on the tree-walker vs the VM with measured numbers in the README; pprof of both; avoiding allocation in the VM's hot loop (preallocated stack and frames, small-int fast path).
- **Stdlib:** `bufio` REPL, `strings.Builder`, `errors.As` with a positioned `SyntaxError`, `embed` for example programs.
- **Libraries:** none — a language implementation is its own library.
- **Testing:** table-driven lexer/parser tests (AST printed back to source), evaluator tests, **differential tests — every program in `testdata/*.tl` must give identical output on both engines** (golden `.out` files), `FuzzParse` (the parser may reject input but must never panic or hang).
- **Teaches:** how languages work end to end; Go interfaces as sum types; recursion and closures; differential testing as the way to trust a second implementation.
- **Scope guard:** ≤ ~1,300 source lines in total; no hashes, no classes, no modules.

### 7. `lsmkv` — an LSM-tree storage engine (a tiny LevelDB) — overall #17
An embedded key-value library: `Open(dir)`, `Put`, `Get`, `Delete`, `Scan(lo, hi)` as an `iter.Seq2`,
durable across crashes. Plus a small REPL CLI and a write/read benchmark tool.

- **Design:** write-ahead log (CRC-framed records, configurable fsync policy) → memtable (skip list, generic) → flush to immutable SSTables (sorted data blocks, sparse index, bloom filter, footer with magic number) → read path newest-to-oldest, merge iterator for scans (`container/heap`), tombstones, compaction (size-tiered full merge when ≥ 4 tables; tombstones dropped only in a full merge), manifest of live tables updated by atomic rename, an advisory file lock so two processes cannot open the same dir.
- **Stdlib:** `os` (`O_APPEND`, `File.Sync`, `Rename`), `encoding/binary` (uvarints), `hash/crc32`, `bufio`, `io.ReaderAt`, `iter`, `container/heap`, `syscall.Flock` behind a `//go:build unix` file.
- **Optimization:** measured effect of the bloom filter on missing-key reads, batch writes vs per-write fsync, benchmarks for sequential vs random writes and hit vs miss reads.
- **Testing:** **model-based test** — random Put/Delete/Get/flush/compact/reopen sequences checked against a `map[string]string`; **crash test** — truncate the WAL at *every* byte offset and prove recovery yields a prefix-consistent state; flipped-byte corruption is detected by CRC; bloom false-positive rate within bounds; `FuzzDecodeBlock` never panics; concurrent readers + one writer under `-race`.
- **Teaches:** durability and `fsync`, why append-only is fast, immutability and merging, tombstones, bloom filters, how LevelDB/RocksDB/Cassandra work inside.
- **Scope guard:** no leveled compaction, no snapshots/MVCC, no block compression — listed as exercises.

### 8. `crdtpad` — collaborative text editing with a sequence CRDT — overall #18
A server + tiny embedded web client (plain JS, one textarea) where several people edit one document
at once, no central ordering, offline edits merge on reconnect. The document is an **RGA**
(Replicated Growable Array): every replica applies its own edits instantly and all replicas
converge regardless of delivery order.

- **Core:** op-based sequence CRDT with Lamport timestamps + replica IDs as unique ordered character IDs, `Insert{id, after, ch}` / `Delete{id}` with tombstones, a pending buffer for ops whose causal predecessor has not arrived, version vectors so a reconnecting client asks "what did I miss", idempotent re-delivery, text materialization.
- **Transport:** `coder/websocket` (context-based API; README compares with the `gorilla/websocket` used in `chathub`), JSON messages, a per-document hub goroutine, op log persisted to disk so the server restarts without losing the document.
- **Testing (the heart):** convergence property tests — N replicas generate random concurrent inserts/deletes, ops are delivered in random order with duplicates, all replicas must end with identical text; the classic concurrent-insert-at-the-same-spot case checked for intention preservation; a 3-client integration test through `httptest` with real WebSockets; `-race`.
- **Teaches:** eventual consistency, CRDT vs operational transformation, Lamport clocks, commutative + idempotent operations, testing distributed algorithms with randomized interleavings. Contrast with `raftkv` (consensus) and `chathub` (central hub).
- **Scope guard:** characters only (no rich text, no cursors), no tombstone GC — discussed in the README, left as an exercise.

### 9. `orderflow` — event-driven order pipeline: outbox, sagas, idempotent consumers — overall #19
`orders` (REST), `payments` and `inventory` services communicating only through NATS JetStream.
An order runs a saga: reserve stock → charge payment → confirm; a failure at any step triggers
compensation (release stock, cancel). The reliability layer is built by hand rather than imported.

- **Patterns:** **transactional outbox** (business row + event row in one DB transaction; a relay polls and publishes), **inbox / idempotency key** (processed-message table), saga orchestration as an explicit state machine (choreography contrasted in the README), compensating actions, event envelope (id, type, aggregate id, correlation id, timestamp), dead-letter subject after max deliveries, retry with backoff.
- **Infra:** embedded `nats-server` with JetStream (in-process, no Docker; `nats.go` client), SQLite via `modernc.org/sqlite`, one DB file per service. README documents the Postgres swap and `FOR UPDATE SKIP LOCKED` for running several relays.
- **Testing (chaos):** crash the relay after publish but before marking the row sent → duplicate delivery → consumers dedupe, final state still correct; payment failure triggers compensation and stock is restored; a poison message lands in the DLQ after N deliveries; many concurrent orders with random failures — invariants hold (stock never negative, every order reaches a terminal state, nothing left reserved); `-race`.
- **Teaches:** why distributed transactions do not exist and what replaces them — effectively-once is at-least-once plus idempotency.

### 10. `raftkv` — replicated key-value store on Raft consensus — overall #20
A 3–5 node cluster: leader election, log replication, durable state, and an HTTP KV API
(`PUT/GET/DELETE /kv/{key}`) whose followers redirect to the leader. The capstone of the second batch.

- **Design:** one event-loop goroutine per node owns all Raft state (the `chathub` lesson — no mutexes in the core); randomized election timeouts; `RequestVote` / `AppendEntries` with the log-matching check and `nextIndex` back-off; commit by majority `matchIndex` (current-term entries only — the Figure 8 rule); apply to the KV state machine; persist `currentTerm`, `votedFor` and the log so restarts are safe; client request IDs deduplicate retried commands; reads go through the log or ReadIndex for linearizability.
- **Seams that make it testable:** an injectable `Clock` and a `Transport` interface — an in-memory network with fault injection (drop, delay, duplicate, partition, isolate a node) for tests, HTTP + JSON for the real cluster; a script/Makefile target starts 3 local nodes.
- **Testing (the point):** at most one leader per term, checked continuously as a safety invariant; leader crash → new leader elected; a minority partition cannot commit while the majority can; after healing, a stale leader's uncommitted entries are overwritten and logs converge; a restarted node keeps committed entries; concurrent clients under random faults — every acknowledged write survives and reads see them; `-race`, run with `-count=5`.
- **Teaches:** consensus, terms, the log-matching property, why majorities, safety vs liveness, and how to test a distributed system deterministically with a simulated network.
- **Scope guard:** snapshots/log compaction and membership changes are exercises, not core.

---

## Working process (per project)

1. I build the project fully, with its README (goals, run instructions, exercise ideas for your rewrite).
2. I run it and show the result.
3. Status in `PROGRESS.md` → `done`, log entry added.
4. You rewrite it in your own `practice/` copy (or from scratch), run it, compare.
5. We move to the next one.

Comments in code follow the repo rule: only where the *why* isn't obvious — no narration.

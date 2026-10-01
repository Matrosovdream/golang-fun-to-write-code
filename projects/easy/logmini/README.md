# E10 logmini — write your own logging package

A reusable leveled logger (`logmini` package) plus a demo app (`cmd/demo`) that consumes it.
You've used `log`, `slog` and `zap` — now build the 100-line version and they stop being magic.

## What it teaches

- **Your first multi-package module**: importable package at the root, binary in `cmd/`,
  exported vs unexported deciding the API surface.
- **`io.Writer` as the destination** — the most important interface in Go. The test logs
  into a `bytes.Buffer`; production logs to stderr; the logger can't tell.
- The **shared sink**: derived loggers (`WithPrefix`) share one mutex, because two mutexes
  over one file means interleaved garbage. The `-race` test proves lines stay whole.
- Format *before* locking — keep the critical section minimal.
- Level filtering before any formatting work; `Level` with iota + `String()`.
- **Struct embedding**: `Server{*logmini.Logger}` promotes `Infof` onto `Server`.
- The package-level default instance pattern (`logmini.Infof` → `std`), exactly how
  stdlib `log` works.
- Injectable clock (`now func() time.Time`) for byte-exact test output.

## Run

```sh
go test -race ./...
go run ./cmd/demo
go run ./cmd/demo 2>/dev/null   # watch the default-logger line disappear (it's on stderr)
```

## Rewrite exercises

1. Rebuild the package; keep the concurrent test green with `-race`.
2. Add `SetOutput(io.Writer)` — what must it lock, and why is swapping mid-flight safe here?
3. Add a `JSONHandler` mode. Now read `log/slog`'s design and see what problem Handlers solve.

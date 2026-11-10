# golang-fun-to-write-code

The name of this repo speaks for itself.

Reference Go projects, easy → hard, built to be **rewritten by hand**: each project is complete,
idiomatic, and covers a specific slice of Go — patterns, popular libraries, concurrency, and
optimization. Retype the code, run it, break it, fill the gaps.

## Layout

```
projects/
  PLAN.md        ← what each project is and what it covers
  PROGRESS.md    ← status table + log
  easy/          ← 15 fundamentals projects, stdlib-first (E01–E15, see PLAN.md)
    wordfreq/ tempconv/ passgen/ guessgame/ dirsize/ grepish/ csvreport/
    jsontool/ todojson/ logmini/ stopwatch/ quizcli/ fetchcli/ pageserve/ multidl/
  medium/        ← 10 projects
    taskcli/ shortlink/ fetchpool/ weathercache/ logparse/
    dagrun/ searchlite/ lbproxy/ sitegen/ obsapi/
  hard/          ← 10 projects
    gobank/ chathub/ grpc-inventory/ jobqueue/ metricsd/
    tinylang/ lsmkv/ crdtpad/ orderflow/ raftkv/
practice/        ← your own rewrites live here
```

Each project has its own `go.mod` and `README.md` with run instructions and rewrite exercises.

## How to use

1. Read the project's README, then its code top-down (`cmd/` → `internal/`).
2. Rewrite it in `practice/<projectname>/` without copy-paste. Peek only when stuck.
3. Run both, compare behavior. Run the tests. Run `go test -race` where the README says to.
4. Mark it `done` in [projects/PROGRESS.md](projects/PROGRESS.md).

## Requirements

- Go 1.22+
- Docker (only `gobank`, `jobqueue` and the optional stack in `obsapi` — Postgres/Redis/Prometheus via docker-compose; fallbacks noted per project)

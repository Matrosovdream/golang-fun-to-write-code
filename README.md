# golang-fun-to-write-code

The name of this repo speaks for itself.

Reference Go projects, medium → hard, built to be **rewritten by hand**: each project is complete,
idiomatic, and covers a specific slice of Go — patterns, popular libraries, concurrency, and
optimization. Retype the code, run it, break it, fill the gaps.

## Layout

```
projects/
  PLAN.md        ← what each project is and what it covers
  PROGRESS.md    ← status table + log
  medium/
    taskcli/ shortlink/ fetchpool/ weathercache/ logparse/
  hard/
    gobank/ chathub/ grpc-inventory/ jobqueue/ metricsd/
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
- Docker (hard level only — Postgres/Redis via docker-compose; fallbacks noted per project)

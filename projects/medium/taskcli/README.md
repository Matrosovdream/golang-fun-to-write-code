# taskcli — task manager CLI

A terminal todo manager with two interchangeable storage backends (SQLite and a JSON file).

## What this project teaches

- **Project layout**: `main.go` → `cmd/` (CLI wiring) → `internal/` (real logic). Nothing outside
  the module can import `internal/` — that's enforced by the compiler.
- **Repository pattern**: `storage.Repository` is an interface; SQLite and JSON both implement it.
  The commands never know which one they use.
- **Factory**: `storage.New(cfg)` picks the implementation.
- **Functional options**: `List(ctx, WithStatus(...), WithLimit(...))` — compare with a `Filter`
  struct argument and note what each approach costs.
- **Sentinel errors + wrapping**: `storage.ErrNotFound`, `fmt.Errorf("...: %w", err)`,
  matched by callers with `errors.Is`.
- **cobra + viper**: subcommands, flags, env vars (`TASKCLI_BACKEND`, `TASKCLI_PATH`),
  optional `~/.taskcli.yaml` config — and the precedence between them.
- **database/sql** with the pure-Go SQLite driver (`modernc.org/sqlite`): schema bootstrap,
  `ExecContext`/`QueryContext`, `sql.NullTime`, `RowsAffected`.
- **Atomic file writes**: temp file + `os.Rename` in the JSON backend.
- **Conformance testing**: one table-driven suite (`storage_test.go`) runs against every backend.

## Run

```sh
go test ./...

go run . --path ./demo.db add "Read the taskcli code" -p 1
go run . --path ./demo.db add "Rewrite it from scratch"
go run . --path ./demo.db list
go run . --path ./demo.db done 1
go run . --path ./demo.db list --all

# same thing on the JSON backend:
go run . --backend json --path ./demo.json add "json works too"
```

## Rewrite exercises

1. Rebuild it without peeking; start from the `Repository` interface.
2. Add an `edit <id> --title --priority` command.
3. Add a third backend (e.g. in-memory, or BoltDB) — the conformance test must pass unchanged.
4. Add `--sort created|priority` using a strategy function `less(a, b task.Task) bool`.

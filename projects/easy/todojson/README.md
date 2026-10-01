# E09 todojson — minimal todo list

`todojson add buy milk` · `list` · `done 1` · `rm 1` — one JSON file, zero dependencies.

This is **taskcli's little sibling**: same idea, no cobra, no SQLite, no interfaces.
Rewrite this one, then read taskcli and see exactly what each library buys.

## What it teaches

- Structs with JSON tags; `*time.Time` + `omitempty` for "not yet" values.
- Marshal/unmarshal round trip as persistence; corrupt-file handling with a wrapped error.
- **Hand-rolled subcommands** on `os.Args` — the thing cobra automates.
- `slices.IndexFunc` / `slices.DeleteFunc` — generic stdlib helpers over hand loops.
- Why `NextID` must persist (the round-trip test fails without it — delete the field and watch).
- Env-var config (`TODO_FILE`) as the simplest knob.

## Run

```sh
go test ./...
go run . add read the todojson code
go run . add rewrite it
go run . list
go run . done 1
go run . list
rm -f todos.json
```

## Rewrite exercises

1. Rebuild it from the command list alone.
2. Add `edit <id> <new title...>` and `clear` (remove all done).
3. Make saves atomic (temp file + rename — steal the trick from taskcli's JSON backend).
4. Then: open `../../medium/taskcli` and list three things cobra/SQLite gave it that this version lacks.

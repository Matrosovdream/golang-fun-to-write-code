# E12 quizcli — timed quiz game

Questions from a CSV, one total time limit; the clock doesn't wait for slow typists.

## What it teaches

- **The canonical "timeout on user input" shape** — and why nothing simpler works:
  reading stdin blocks, so a goroutine forwards answers over a channel, and `select`
  races each answer against the deadline. Your first *necessary* goroutine.
- `time.After` placement: started once *before* the loop = one quiz deadline; inside
  the loop it would quietly become a per-question timer. Classic bug, worth doing wrong
  once on purpose.
- Channel close as EOF signaling (piped input ending ≠ timeout — different endings, both tested).
- Testing all three endings with scripted readers — including `io.Pipe` to simulate a
  user who never answers.
- From here on: run tests with `-race`.

## Run

```sh
go test -race ./...
go run .                          # 30s default
go run . -limit 15s -shuffle
```

## Rewrite exercises

1. Rebuild `run` from the shape description above; port the three ending tests.
2. Move `time.After` inside the loop, run the tests, understand the failure, move it back.
3. Add a per-question time budget *on top of* the total one (two timers in one select).
4. Show remaining time in the prompt by adding a 1s ticker case to the select.

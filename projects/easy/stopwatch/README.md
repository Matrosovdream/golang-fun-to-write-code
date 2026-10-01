# E11 stopwatch — stopwatch & pomodoro timer

`stopwatch` (Enter = lap, Ctrl+C = finish) · `stopwatch -pomodoro 25m` (live countdown + bell).

## What it teaches

- The `time` package properly: `Duration` arithmetic, `time.Until`, rounding for display,
  and `flag.Duration` parsing ("25m", "90s") for free.
- **`Ticker` vs `Timer`**: repeats vs once — the countdown uses both and shows who stops what.
- **First `select`**: merging "user pressed Enter", "tick", "deadline", and "Ctrl+C" into one
  ordered event stream.
- Blocking stdin turned into channel events by a goroutine (the only way to `select` on input).
- `signal.NotifyContext` for clean Ctrl+C in both modes.
- Terminal tricks: `\r` repaints one line, `\a` rings the bell.
- Testing time-based code with *short real durations* and asserting elapsed bounds —
  no sleeps longer than 50ms in the whole suite.

## Run

```sh
go test ./...
go run .                  # Enter a few times, then Ctrl+C
go run . -pomodoro 10s    # short demo; try 25m for real
```

## Rewrite exercises

1. Rebuild it; keep both mode functions pure enough for the same tests.
2. Show laps as a table at the end: lap #, split, total, delta to best lap.
3. Pomodoro cycles: work/break/work (`-breaks 5m -cycles 4`), announcing each phase.
4. Replace the Ticker repaint with updating only when the displayed second actually changes — what does that save?

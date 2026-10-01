# E04 guessgame — number guessing game

The program picks 1–100 (or 1–N: `guessgame 1000`), you binary-search it; best score persists.

## What it teaches

- Interactive stdin loops with `bufio.Scanner`: prompt, read, validate, **re-prompt without
  punishing** (invalid input doesn't count as an attempt).
- Designing for testability from day one: `play(r, w, secret, max)` touches no globals, so
  the test scripts a whole game through a `strings.Reader`.
- `strconv.Atoi` error handling as control flow; `strings.TrimSpace` before parsing, always.
- A tiny state file (`~/.guessgame_best`): read-parse-compare-write, tolerant of absence
  and corruption.
- `math/rand/v2` for games (contrast with passgen's `crypto/rand`).

## Run

```sh
go test ./...
go run .          # interactive
go run . 1000     # bigger range
```

## Rewrite exercises

1. Rebuild it; write `TestPlayScriptedGame` first and code until it passes.
2. Add a `-lives N` hard mode that ends the game after N wrong guesses.
3. Reverse mode: *you* pick the number, the program binary-searches and you answer h/l/c.

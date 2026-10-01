# E06 grepish — mini grep

`grepish -i -n -r "pattern" paths...` — matching lines with file:line prefixes, stdin piping,
and real grep's exit-code contract.

## What it teaches

- **`regexp`**: compile once outside the loop; `(?i)` for case-insensitivity instead of
  lowercasing every line; `re.Match` on bytes (no string conversion in the loop).
- **Exit codes as API**: 0 matched / 1 no match / 2 error — this is what makes
  `if grepish -r TODO . ; then` work in shell scripts.
- **stdout vs stderr discipline**: matches to stdout, problems to stderr, so pipelines
  stay clean.
- Flag combinations shaping one output line (`name:line:text` variants).
- `filepath.WalkDir` for `-r`, reading stdin when no paths given — same `search` function
  serves both (io.Reader again).

## Run

```sh
go test ./...
go run . -n "func" main.go
go run . -r -i "todo" .
echo "an ERROR happened" | go run . ERROR ; echo "exit: $?"
go run . absent main.go ; echo "exit: $?"
```

## Rewrite exercises

1. Rebuild it; keep the exit-code contract (test it from shell).
2. Add `-v` (invert match) and `-c` (count only) — watch how the output-shaping switch grows, then refactor it.
3. Add `--include "*.go"` glob filtering for `-r` (see `filepath.Match`).

# E03 passgen — password & passphrase generator

`passgen` → random passwords; `passgen -words 4` → `maple-canyon-ember-fox` style passphrases.

## What it teaches

- **`crypto/rand` vs `math/rand/v2`** — and why the choice is not a style preference when
  the output is a secret (read the `randInt` comment).
- **`go:embed`**: the wordlist compiles into the binary; no data files to ship or mislocate.
- `strings.Builder` with `Grow` — efficient string assembly.
- Bytes vs runes: indexing an ASCII alphabet by byte is fine *because* it's ASCII.
- Flag combinations building behavior (`-no-digits`, `-words` switching modes).
- **Property-based testing**: random output can't be golden-filed — assert invariants
  (length, alphabet membership, distinctness) instead.

## Run

```sh
go test ./...
go run .
go run . -len 32 -n 1 -no-symbols
go run . -words 5 -sep .
```

## Rewrite exercises

1. Rebuild it; keep `password`/`passphrase` pure so the property tests port over.
2. Add `-require-each` that guarantees ≥1 char from every enabled class (tricky to do without bias — think before coding).
3. Compute and print the entropy in bits for whichever mode ran.

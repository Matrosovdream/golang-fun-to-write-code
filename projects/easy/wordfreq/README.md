# E01 wordfreq — word frequency counter

Count the most frequent words of a file (or stdin) and print a top-N table.

## What it teaches

- Maps for counting, slices for ordering, and converting between them.
- `sort.Slice` with a two-level comparison (count desc, then alphabetical) for stable output.
- `bufio.Scanner`, `strings.FieldsFunc` with a rune predicate, `unicode` classification.
- **`io.Reader` as the unifying type**: files, stdin, and `io.MultiReader` all look the same
  to `run`, and tests feed it a `strings.Reader`.
- `text/tabwriter` for aligned terminal tables.
- Stdlib testing: table-driven tests, `reflect.DeepEqual`, and the **golden file** pattern
  with an `-update` flag.

## Run

```sh
go test ./...
echo "the cat and the dog and the bird" | go run . -top 3
go run . -top 10 -min 4 somebook.txt
```

## Rewrite exercises

1. Rebuild it; keep `run` testable (no `os.Stdout` inside).
2. Add `-bottom` to show the rarest words instead.
3. Stream two files and verify `io.MultiReader` counts across the boundary correctly.

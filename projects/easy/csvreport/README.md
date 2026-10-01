# E07 csvreport — sales CSV aggregator

Read `date,region,product,amount` rows, print per-region and per-month totals with shares.

## What it teaches

- **`encoding/csv`**: header handling, `FieldsPerRecord` enforcing column counts, reading
  row-by-row until `io.EOF`.
- **Row-numbered errors** (`row 17: bad date: ...`) via `%w` wrapping — the habit that
  saves hours on real data files.
- `time.Parse` and the reference-date layout (`2006-01-02` *is* the format string);
  reusing `Format` for month bucketing.
- Map-based aggregation, and why every report needs `sortedKeys` (map order is random
  on purpose).
- Comparing floats in tests with a tolerance, never `==`.
- When `float64` money is acceptable (a report) vs not (a ledger — see gobank).

## Run

```sh
go test ./...
go run . testdata/sales.csv
```

## Rewrite exercises

1. Rebuild it; make the row-number tests pass first.
2. Add `-by product` and `-csv out.csv` (write the summary with `csv.Writer`).
3. Switch parsing to streaming aggregation (no `[]Sale` kept) — what do you lose?

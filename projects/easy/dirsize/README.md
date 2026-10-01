# E05 dirsize — disk usage tool

`dirsize ~/Downloads` → total size, file/dir counts, and the biggest files, human-readable.

## What it teaches

- **`filepath.WalkDir`** and the `io/fs` types (`fs.DirEntry`, `fs.SkipDir`) — the modern
  way to walk trees (`WalkDir` beats the older `Walk`: no Stat per entry unless asked).
- **Error policy as a design decision**: unreadable entries are counted and skipped, never
  fatal — and the test pins that behavior down.
- Distinguishing regular files from symlinks/sockets (`d.Type().IsRegular()`).
- The classic `human()` bytes formatter (loop + `"KMGTPE"[exp]` indexing trick).
- Building test fixtures with `t.TempDir()` — self-cleaning, parallel-safe.

## Run

```sh
go test ./...
go run . ~/Downloads
go run . -top 5 /usr/local
```

## Rewrite exercises

1. Rebuild it; keep the skip-and-report policy test green.
2. Add `-dirs` mode: biggest *directories* by cumulative size (you'll need a map of running totals keyed by parent).
3. Parallelize: one goroutine per top-level subdirectory, merge results. Measure on a big tree — does it help on your disk?

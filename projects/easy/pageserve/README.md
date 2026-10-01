# E14 pageserve — first web server

Serves a directory: HTML listing at `/`, files at `/files/…`, request counts at `/stats`.

## What it teaches

- `net/http` server basics: `ServeMux` with 1.22+ patterns (`GET /{$}` for exactly-root),
  `http.FileServerFS` + `StripPrefix`, `http.Error`.
- **`html/template`, and why not `text/template` for HTML**: escaping is contextual and
  automatic — the test plants a file named `<script>…` and proves it renders inert.
- **First shared state under concurrency**: the server runs handlers on many goroutines,
  so even a humble hit counter needs a mutex. Note the stats handler copying under lock
  and rendering after.
- Middleware, minimum viable version (compare with shortlink's chain later).
- `fs.FS` everywhere: `os.DirFS` in main, `fstest.MapFS` in tests — same code, zero temp dirs.
- `httptest.NewServer` end-to-end handler tests.

## Run

```sh
go test -race ./...
go run . -dir .        # then open http://localhost:8088
curl -s localhost:8088/stats
```

## Rewrite exercises

1. Rebuild it; keep the XSS test green.
2. Add directory browsing under `/files/sub/...` to the listing page (template recursion or query param).
3. Replace the mutex counter with `sync/atomic` per path — why is that harder than it sounds? (Hint: the map itself.)
4. Add an `Last-Modified`/`If-Modified-Since` check to the listing page.

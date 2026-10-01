# E13 fetchcli — tiny curl

`fetchcli -v -o page.html -timeout 5s https://go.dev` — status + headers on stderr, body
streamed to stdout or a file.

## What it teaches

- **`http.Client` done right from day one**: never bare `http.Get` (the default client has
  no timeout), always `defer resp.Body.Close()`.
- **Streaming with `io.Copy`** vs `io.ReadAll`: a 10GB download never sits in memory.
- Redirects: the client follows them; `resp.Request.URL` reveals where you landed.
- A 4xx/5xx response is *not* a Go error — deciding what counts as failure is your job
  (and draining a little before bailing keeps the connection reusable).
- Body to stdout, diagnostics to stderr — so `fetchcli url | jq` just works.
- `httptest.Server` for client testing: happy path, redirect, 404, and a bounded timeout
  test that proves the client gives up.

## Run

```sh
go test ./...
go run . -v https://example.com
go run . -o /tmp/go.html https://go.dev && ls -lh /tmp/go.html
go run . -v https://httpstat.us/404 ; echo "exit: $?"
```

## Rewrite exercises

1. Rebuild it; keep all four tests passing.
2. Add `-m POST -d @file` (watch what changes about retrying and bodies).
3. Print a progress line during big downloads (wrap the body in a counting io.Reader — write your first custom Reader).
4. Add `-H "Key: Val"` repeatable header flags (you'll need a custom `flag.Value`).

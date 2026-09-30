# shortlink — URL shortener REST API

`POST /api/links` → short code · `GET /{code}` → 302 redirect · `GET /api/links/{code}` → stats.

## What this project teaches

- **Service structure**: transport (`handler`) → business logic (`service`) → storage (`repo`),
  wired by hand in `main.go` — dependency injection without a framework.
- **chi router** + middleware chain: request IDs, structured access log, panic recovery —
  and why middleware order matters.
- **Writing middleware**: the `statusWriter` wrapper shows why you must wrap
  `http.ResponseWriter` to observe the response.
- **log/slog**: structured JSON logging from the standard library.
- **Central error mapping**: domain errors (`repo.ErrNotFound`, `service.ErrInvalidURL`)
  translated to HTTP codes in exactly one place with `errors.Is`.
- **Validation**: `go-playground/validator` struct tags on the request DTO.
- **Graceful shutdown**: `signal.NotifyContext` + `srv.Shutdown` with a deadline.
- **http.Server timeouts**: why a server without them leaks goroutines to slow clients.
- **Concurrency-safe storage**: `sync.RWMutex` in-memory repo; atomic `hits+1` in SQL
  (vs. read-modify-write, which races).
- **httptest**: black-box tests through real HTTP, including inspecting a 302 without
  following it.

## Run

```sh
go test ./...

go run ./cmd/server                                  # in-memory backend
SHORTLINK_BACKEND=sqlite go run ./cmd/server         # persistent backend

# in another terminal:
curl -s -X POST localhost:8080/api/links -d '{"url":"https://go.dev/doc/"}' | jq
curl -i localhost:8080/<code>
curl -s localhost:8080/api/links/<code> | jq
```

## Rewrite exercises

1. Rebuild from the `Repo` interface outward.
2. Add `DELETE /api/links/{code}` and an expiry (`expires_at`, reject resolves after it).
3. Add a rate-limit middleware (fixed window per IP) — then compare with `x/time/rate`.
4. Swap chi for stdlib `http.ServeMux` (1.22+ patterns like `GET /{code}`) — what do you lose?

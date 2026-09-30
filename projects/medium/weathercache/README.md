# weathercache — API client with a caching layer

A weather CLI on the free [Open-Meteo](https://open-meteo.com) API (no key needed), with an
LRU+TTL cache and a retrying HTTP transport in front.

## What this project teaches

- **Decorator pattern, twice, at two different layers**:
  - `weather.Cached` wraps the `Provider` *interface* (app level);
  - `httpx.RetryTransport` wraps `http.RoundTripper` (transport level) — retries with
    exponential backoff + jitter, and why only GET/HEAD can be retried blindly.
- **The interface seam**: `Provider` is satisfied by the real client, the cache decorator,
  and the mock. Everything composes in `main.go` like an onion.
- **Generics for real**: `cache.LRU[K comparable, V any]` — `container/list` + map, lazy TTL
  expiry, and why `Get` needs a full Mutex (it mutates recency) not an RWMutex.
- **Injectable clock** (`WithClock`) — TTL tests run instantly, no `time.Sleep`.
- **Both ways to test HTTP clients**, side by side:
  - `openmeteo_test.go`: fake server via `httptest` (tests URL building + JSON decoding);
  - `cached_test.go`: generated interface mock (`mockery`-style, testify/mock) with
    call-count assertions (`Once()`, `Twice()`).
- **`http.Client` discipline**: timeouts always, transport injection, closing bodies before
  retry so connections get reused.

## Run

```sh
go test ./...

go run ./cmd/weather Berlin Tokyo
# the second lookup per city comes from the cache — compare the timings
```

## Rewrite exercises

1. Rebuild the LRU from scratch (map + doubly linked list) without opening the original.
2. Add `singleflight` (`golang.org/x/sync/singleflight`) so concurrent misses for the same
   city fire one upstream request, not N.
3. Add a `-json` output flag; keep the human format the default.
4. Regenerate the mock with real mockery (`go generate ./...`) and diff it against the
   checked-in one.
5. Make the retry transport honor a `Retry-After` header.

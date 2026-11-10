# lbproxy — L7 reverse proxy & load balancer

`lbproxy -config lb.yaml`: routes requests by host and path prefix to pools of backends, balances them
with one of four strategies, keeps unhealthy backends out of rotation, retries idempotent requests on
another backend, and reloads its config on `SIGHUP` without dropping a request.

## What this project teaches

- **Strategy pattern**: `lb.Balancer` (`internal/lb/balancer.go`) is one method, `Pick(key, candidates)`.
  `Pool.Pick` filters out unhealthy and already-tried backends *first*, so no balancer ever knows about
  health or retries. Four implementations: round robin, smooth weighted round robin, least
  connections, consistent hash.
- **Smooth weighted round-robin** (`wrr.Pick`): nginx's algorithm. `current += effective`, the largest
  `current` wins and pays back the total. Weights 5:1:1 give `a a b a c a a`, not `a a a a a b c`
  (`TestSmoothWRRSequence` pins the exact sequence). A failure halves the *effective* weight and each
  round gives one point back (`Penalize`), so a flaky backend ramps up again instead of snapping to full load.
- **Consistent hashing** (`internal/lb/ring.go`): `replicas*weight` virtual points per backend, sorted by
  `crc32`, key lookup with `sort.Search`, then walk clockwise to the first point whose owner is a
  *candidate*. That skip is provably the same as rebuilding the ring without the dead node
  (`TestRingCandidateFilterEqualsRebuild`), so health changes remap minimally too.
  `TestRingMinimalRemapWhenNodeLeaves` proves it on 10,000 keys: removing 1 of 5 nodes moves exactly the
  keys that node owned (2,162 = 21.6%) and **no key moves between surviving nodes**; adding a 5th moves
  only keys *to* the new node. With 160 points per node the spread is uneven (one node owned 1,683 of
  10,000 keys, another 2,369) because placement is random: more replicas tighten it, at the cost of memory.
- **Least connections**: atomic in-flight counters on `Backend` (`Acquire`/`Release` around each
  attempt); the scan starts at a rotating offset so ties spread instead of piling onto backend 0.
- **State machine with hysteresis** (`Backend.Observe`): a probe that agrees with the current state
  resets the streak, `fall` (or `rise`) disagreeing probes in a row flip it. Table-driven test
  in `backend_test.go`.
- **Active vs passive health, two independent signals** (`Backend.Available` needs both):
  `health.Checker` probes on a ticker you inject (`Run(ctx, tick)`; tests call `Round` directly, no
  sleeping); `Backend.Report` ejects after N *consecutive real request failures* for a cooldown, then
  is half-open: one more failure re-ejects at once, one success closes it.
- **`httputil.ReverseProxy` internals** (`Proxy.try`): `Rewrite` with `SetURL` + `SetXForwarded`
  (unlike the deprecated `Director`, hop-by-hop headers and inbound `X-Forwarded-*` are stripped
  *before* `Rewrite` runs, so clients cannot spoof them: see `TestForwardingHeadersPathAndHost`);
  `ErrorHandler` that records the error and writes nothing, so the caller decides what the client sees;
  `ModifyResponse` that *returns an error* for 502/503/504 to make ReverseProxy discard the response
  unwritten, which is the trick that makes retrying a bad status possible.
- **Retry rules** (`Proxy.forward`): only GET/HEAD/OPTIONS *without a body*, only on a backend not
  yet tried, at most `retries` extra attempts; the last attempt lets the backend's own 503 through
  instead of masking it with a generic 502. A client that hangs up is not a backend failure.
- **`http.Transport` tuning** (`proxy.New`): `MaxIdleConnsPerHost` defaults to 2, which makes a proxy
  reconnect constantly; dial/handshake/response-header timeouts.
- **Hot reload by atomic swap** (`proxy.go`, `snapshot.go`): the whole config (routes, pools, balancer
  state, checkers) is one immutable `snapshot` behind `atomic.Pointer`. `Reload` builds a complete new one
  and swaps; a request loads the pointer once and never sees a mix; in-flight requests finish on the
  snapshot they started with; the old snapshot's checkers are cancelled. `config.Parse` validates
  everything first (`KnownFields(true)`, every problem reported via `errors.Join`), so a bad file never
  reaches the proxy. Tests: `TestInFlightRequestFinishesOnTheConfigItStartedWith`,
  `TestInvalidReloadKeepsTheOldConfigRunning`, `TestReloadUnderConcurrentLoad` (8 clients, 200 reloads,
  `-race`), `TestReloadStopsTheOldHealthCheckers`.
- **Decorator**: `instrument` (`admin.go`) wraps the proxy handler to count and log. `statusRecorder.Unwrap`
  is what lets `http.ResponseController` still flush streams through the wrapper
  (`TestStreamingResponsesAreFlushedThroughTheDecorator`).
- **Operations**: `/lb/status` JSON (per backend: health, ejected, in-flight, requests, failures),
  `/debug/vars` (`expvar` counters), `signal.NotifyContext` + `srv.Shutdown` (an in-flight 3s request
  completed with 200 after `SIGTERM`; new connections were refused), `SIGHUP` loop in `cmd/lbproxy`.

## Run

```sh
go test -race ./...
go test -v -run TestRing ./internal/lb        # prints the key-spread / remap numbers above

BIN=$(mktemp -d); go build -o "$BIN" ./cmd/...
cp lb.example.yaml "$BIN/lb.yaml"
"$BIN/demo-backend" -name b1 -addr 127.0.0.1:9101 -delay 200ms 2>/dev/null &
"$BIN/demo-backend" -name b2 -addr 127.0.0.1:9102 2>/dev/null &
"$BIN/demo-backend" -name b3 -addr 127.0.0.1:9103 2>/dev/null &
B3=$!
sleep 1
"$BIN/lbproxy" -config "$BIN/lb.yaml" 2>"$BIN/proxy.log" &
PROXY=$!
sleep 1

hits()   { for _ in $(seq "$1"); do curl -s "${@:2}"; done | cut -d: -f1 | sort | uniq -c | tr '\n' ' '; echo; }
status() { curl -s localhost:8102/lb/status | jq -r '.pools[] | select(.name=="web") | .backends[] | "\(.url)  healthy=\(.healthy) ejected=\(.ejected) inflight=\(.inflight) requests=\(.requests) failures=\(.failures)"'; }

echo '--- round robin, six requests'
for _ in $(seq 6); do curl -s localhost:8102/; done

echo '--- weighted round robin 5:1:1 (route by Host), 14 requests in order'
for _ in $(seq 14); do curl -s -H 'Host: api.local' localhost:8102/ | cut -d: -f1; done | tr '\n' ' '; echo

echo '--- consistent hash: the same X-User always lands on the same backend'
for u in alice bob carol alice bob carol; do printf '%s -> ' $u; curl -s -H "X-User: $u" localhost:8102/sticky/x; done

echo '--- least connections: 30 concurrent requests, b1 is 200ms slower'
seq 30 | xargs -P 30 -I{} curl -s localhost:8102/busy/x | cut -d: -f1 | sort | uniq -c

echo '--- b2 starts failing /healthz: active check takes it out after fall=2 probes'
curl -s -X POST 127.0.0.1:9102/healthz/down
sleep 3; status
for _ in $(seq 6); do curl -s localhost:8102/; done

echo '--- b2 recovers after rise=2 good probes'
curl -s -X POST 127.0.0.1:9102/healthz/up
sleep 3; status
for _ in $(seq 6); do curl -s localhost:8102/; done

echo '--- b3 is killed: GETs are retried on another backend, POSTs are not'
kill $B3; wait $B3 2>/dev/null
for _ in $(seq 6); do curl -s -o /dev/null -w '%{http_code} ' localhost:8102/; done; echo
for _ in $(seq 6); do curl -s -o /dev/null -w '%{http_code} ' -X POST localhost:8102/; done; echo
status
sleep 3; echo 'two failed probes later:'; status

echo '--- b3 restarts; passive cooldown (10s) and rise=2 probes bring it back'
"$BIN/demo-backend" -name b3 -addr 127.0.0.1:9103 2>/dev/null &
sleep 12; status
for _ in $(seq 6); do curl -s localhost:8102/; done

echo '--- expvar'
curl -s localhost:8102/debug/vars | jq -c .lbproxy

echo '--- hot reload (SIGHUP): heavy pool weights 5:1:1 become 1:1:3'
echo -n 'before: '; hits 14 -H 'Host: api.local' localhost:8102/
sed -i.bak -e 's|9101", weight: 5}|9101", weight: 1}|' -e 's|9103", weight: 1}|9103", weight: 3}|' "$BIN/lb.yaml"
kill -HUP $PROXY; sleep 1
echo -n 'after:  '; hits 15 -H 'Host: api.local' localhost:8102/

echo '--- a broken config on SIGHUP is rejected, the old one keeps running'
printf 'listen: 127.0.0.1:8102\nroutes: [{pool: nope}]\npools: {}\n' > "$BIN/lb.yaml"
kill -HUP $PROXY; sleep 1
grep 'reload failed' "$BIN/proxy.log" | cut -c1-170
echo -n 'still serving: '; hits 15 -H 'Host: api.local' localhost:8102/

echo '--- proxy log'
grep -E 'health changed|ejected|reloaded|failed' "$BIN/proxy.log" | cut -c1-150

kill $(jobs -p); rm -rf "$BIN"
```

Needs `jq` (ships with macOS) and ports 8102 and 9101-9103 free. Config shape: see `lb.example.yaml`;
defaults are `strategy: round_robin`, `weight: 1`, `retries: 0` (off), `passive: {max_fails: 3, cooldown: 10s}`,
and active checks only run if a `health:` block exists (defaults `/healthz`, 5s, rise 2, fall 3).
Routes: a host-specific route beats a wildcard host, then the longest `path_prefix` wins.
`/lb/status` and `/debug/vars` are reserved on the proxy's own port.

What that session printed (real output; timestamps trimmed):

```text
--- round robin, six requests
b1: GET /
b2: GET /
b3: GET /
b1: GET /
b2: GET /
b3: GET /
--- weighted round robin 5:1:1 (route by Host), 14 requests in order
b1 b1 b2 b1 b3 b1 b1 b1 b1 b2 b1 b3 b1 b1 
--- consistent hash: the same X-User always lands on the same backend
alice -> b3: GET /sticky/x
bob -> b2: GET /sticky/x
carol -> b1: GET /sticky/x
alice -> b3: GET /sticky/x
bob -> b2: GET /sticky/x
carol -> b1: GET /sticky/x
--- least connections: 30 concurrent requests, b1 is 200ms slower
   1 b1
  17 b2
  12 b3
--- b2 starts failing /healthz: active check takes it out after fall=2 probes
b2: healthz down=true
http://127.0.0.1:9101  healthy=true ejected=false inflight=0 requests=2 failures=0
http://127.0.0.1:9102  healthy=false ejected=false inflight=0 requests=2 failures=0
http://127.0.0.1:9103  healthy=true ejected=false inflight=0 requests=2 failures=0
b1: GET /
b3: GET /
b1: GET /
b3: GET /
b1: GET /
b3: GET /
--- b2 recovers after rise=2 good probes
b2: healthz down=false
http://127.0.0.1:9101  healthy=true ejected=false inflight=0 requests=5 failures=0
http://127.0.0.1:9102  healthy=true ejected=false inflight=0 requests=2 failures=0
http://127.0.0.1:9103  healthy=true ejected=false inflight=0 requests=5 failures=0
b1: GET /
b2: GET /
b3: GET /
b1: GET /
b2: GET /
b3: GET /
--- b3 is killed: GETs are retried on another backend, POSTs are not
200 200 200 200 200 200 
502 200 200 200 200 200 
http://127.0.0.1:9101  healthy=true ejected=false inflight=0 requests=11 failures=0
http://127.0.0.1:9102  healthy=true ejected=false inflight=0 requests=11 failures=0
http://127.0.0.1:9103  healthy=true ejected=true inflight=0 requests=10 failures=3
two failed probes later:
http://127.0.0.1:9101  healthy=true ejected=false inflight=0 requests=11 failures=0
http://127.0.0.1:9102  healthy=true ejected=false inflight=0 requests=11 failures=0
http://127.0.0.1:9103  healthy=false ejected=true inflight=0 requests=10 failures=3
--- b3 restarts; passive cooldown (10s) and rise=2 probes bring it back
http://127.0.0.1:9101  healthy=true ejected=false inflight=0 requests=11 failures=0
http://127.0.0.1:9102  healthy=true ejected=false inflight=0 requests=11 failures=0
http://127.0.0.1:9103  healthy=true ejected=false inflight=0 requests=10 failures=3
b3: GET /
b1: GET /
b2: GET /
b3: GET /
b1: GET /
b2: GET /
--- expvar
{"config_loads":1,"requests":86,"responses_2xx":85,"responses_5xx":1,"retries":2,"upstream_errors":1}
--- hot reload (SIGHUP): heavy pool weights 5:1:1 become 1:1:3
before:   10 b1    2 b2    2 b3 
after:     3 b1    3 b2    9 b3 
--- a broken config on SIGHUP is rejected, the old one keeps running
time=2026-11-10T10:05:50.442+07:00 level=ERROR msg="reload failed, keeping the running config" err="invalid config:\nroutes[0]: unknown pool \"nope\""
still serving:    3 b1    3 b2    9 b3 
--- proxy log
time=2026-11-10T10:05:24.960+07:00 level=WARN msg="backend health changed" pool=web backend=127.0.0.1:9102 healthy=false
time=2026-11-10T10:05:27.960+07:00 level=WARN msg="backend health changed" pool=web backend=127.0.0.1:9102 healthy=true
time=2026-11-10T10:05:30.606+07:00 level=WARN msg="backend ejected" pool=web backend=127.0.0.1:9103
time=2026-11-10T10:05:31.959+07:00 level=WARN msg="backend health changed" pool=web backend=127.0.0.1:9103 healthy=false
time=2026-11-10T10:05:35.960+07:00 level=WARN msg="backend health changed" pool=web backend=127.0.0.1:9103 healthy=true
time=2026-11-10T10:05:48.716+07:00 level=INFO msg="config reloaded" path=/var/folders/xs/7xmvk0xx67l96p01c24hbrr40000gn/T/tmp.OCcHdNq9Zu/lb.yaml
time=2026-11-10T10:05:50.442+07:00 level=ERROR msg="reload failed, keeping the running config" err="invalid config:\nroutes[0]: unknown pool \"nope\""
```

Two things worth reading in that output: with `b3` dead the six GETs all return 200 (retried) while
the one POST that landed on `b3` is a 502 (never retried); and the ejection (`ejected=true`) happens
from three real failures *before* the active checker has even noticed (`healthy=true`), which is why both
signals exist.

## Rewrite exercises

1. Rewrite `Proxy.forward` and `Proxy.try` from memory. The retry rule is the exam: idempotent method,
   no body, a backend not yet tried, last attempt passes the response through, a client hang-up is not
   the backend's fault.
2. Share backend state between pools: key backends by URL inside a snapshot so two pools pointing at
   the same URL share health and in-flight counters, and carry that state across reloads by URL.
3. Add a fail-open ("panic") mode: when every backend is ejected or down, send to all of them anyway
   instead of answering 503 (nginx resets its failure marks, Envoy has a panic threshold). Write the test first.
4. Replace the per-attempt `ReverseProxy` in `try` with one shared instance that reads the attempt's
   backend and result from the request context. Benchmark allocations before and after.
5. Add a fifth strategy, power-of-two-choices (pick two candidates at random, take the one with fewer
   in-flight), and a test that it beats round robin when one backend is slow.

## Deliberately left out

- No fail-open when all backends are down (503), no retry budget or backoff, no per-pool response
  timeout (one global 30s header timeout in the `Transport`).
- Health state is per pool: the same URL in two pools is two `Backend`s with separate counters.
- `least_conn` ignores weights; `listen` is read at startup only (changing it needs a restart).
- Admin endpoints share the proxy port; production would bind them to a separate listener.
- Not verified: HTTPS upstreams, WebSocket upgrades, HTTP/2, long soak runs. `ReverseProxy` supports
  them, but nothing here exercises them.

## Size

About 1,040 source lines (850 without comments and blanks), above the 400-700 medium target, plus
about 1,240 lines of tests. The spec is several projects at once, and the lines split like this:
`internal/lb` 337 (health state machines, four balancers, ring, pool), `internal/proxy` 353
(retrying request path 160, snapshots and reload 112, status/expvar/decorator 81), `internal/config`
156 (loading plus full validation), `internal/health` 68, `cmd/lbproxy` 72, and `cmd/demo-backend` 55,
which only exists for the demo. I looked for dead code, unused options and duplication and found none
worth removing without dropping a spec item.

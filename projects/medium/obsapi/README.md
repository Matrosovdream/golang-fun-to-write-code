# obsapi — observability for a Go service: metrics, traces, correlated logs

Two tiny HTTP services (`gateway` :8104 → `catalog` :8105) instrumented the way production
services are: Prometheus RED metrics, OpenTelemetry traces that cross the process boundary,
and `slog` lines that carry the trace ID. Everything works with just the Go toolchain (spans
print to stdout, metrics are scraped with `curl`); `docker-compose.yml` adds Prometheus and
Jaeger if you want the real UIs.

## What this project teaches

- **Middleware stack** — `Obs.Handler` (`internal/obs/http.go`): `otelhttp` (span +
  `traceparent` extraction) → `instrument` (metrics + access log) → mux. Tracing is outermost
  so the access-log line runs *inside* the server span. `/metrics`, `/healthz`, `/readyz` are
  dispatched beside the stack, so probes make no spans and no RED series.
- **Cardinality is the production footgun** — `routeLabel` uses the matched mux pattern
  (`r.Pattern`, Go 1.23+), never `r.URL.Path`; unmatched requests all share `unmatched`, and
  `methodLabel` bounds the method too. `TestUnknownPathsDoNotMintRouteLabels` fires 200 random
  paths and proves no new series appear. Break it: swap in `r.URL.Path` and watch it go red.
  `instrument` must wrap the mux directly — any middleware between them that calls
  `r.WithContext` copies the request and hides `r.Pattern` (every route becomes `unmatched`).
- **RED metrics** — counter, histogram, in-flight gauge (`internal/obs/metrics.go`). Buckets
  are chosen, not defaulted: the defaults start at 5ms and these handlers answer in ~1ms.
  The registry is private (not the global default), so tests see exactly their own series.
- **Custom collector** — `Collector` in `internal/catalog/repo.go` exposes an atomic the repo
  already keeps (`catalog_repo_reads_total`) at scrape time instead of mirroring it.
- **Provider / exporter separation** — `NewExporter` is the only code that knows where spans
  go (stdout, or OTLP/HTTP when `OTEL_EXPORTER_OTLP_ENDPOINT` is set); `NewTracerProvider`
  takes any `SpanProcessor`, which is how the tests plug in `tracetest.SpanRecorder`.
- **Sampling** — `ParentBased(TraceIDRatioBased(ratio))`: the edge decides, downstream
  follows, so a trace is never stored half-way (`TestSamplingDecisionFollowsTheParent`).
- **Context is the transport** — `Obs.Transport` injects `traceparent` on the way out,
  `otelhttp` extracts it on the way in; the catalog's server span ends up a child of the
  gateway's client span (`TestOneTraceAcrossBothServices`). Propagator and tracer provider are
  passed explicitly, never installed globally, so two services can share one test process.
- **Decorator** — `tracedRepo` (`internal/catalog/service.go`) wraps a `Repository` and is one,
  so handler → service → repo gives three spans without the repo knowing. `recordErr` shows
  the rule: a caller's mistake (unknown id) is an *event*, our failure is `RecordError` +
  status `Error`.
- **Logs that lead to traces** — `traceHandler` (`internal/obs/logging.go`) stamps
  `trace_id`/`span_id` from the context. It only works with `log.InfoContext(ctx, …)`; plain
  `log.Info` has no span to find. `WithAttrs`/`WithGroup` must re-wrap or `log.With` loses it.
- **Operations** — `Health` separates liveness from readiness; `Serve` flips readiness to 503,
  waits `-drain`, then `Shutdown`s; `Run` flushes the tracer provider last, because the batch
  processor holds spans in memory.
- **Testing without sleeping** — `httptest.Server.Close` blocks until every handler has
  returned, which is also when every server span has ended, so the recorders can be read
  right after it. `internal/obs/obstest` reads series back out of a registry.

## Run

```sh
go test -race ./...

go build -o /tmp/obsapi-bin/ ./cmd/...
/tmp/obsapi-bin/catalog -fail-rate 0.3 > /tmp/catalog.out & CAT=$!   # 30% of repo calls fail
/tmp/obsapi-bin/gateway > /tmp/gateway.out & GW=$!
until curl -sf localhost:8104/readyz && curl -sf localhost:8105/readyz; do sleep 0.2; done

curl -s localhost:8104/products/p-1
curl -s localhost:8104/products/nope
curl -s localhost:8104/products
for i in $(seq 30); do curl -s -o /dev/null localhost:8104/products/p-$((i % 4 + 1)); done
for p in wp-login.php admin .env a/b/c; do curl -s -o /dev/null localhost:8104/$p; done

curl -s localhost:8104/metrics | grep -E '^(http_requests_total|http_requests_in_flight)'
curl -s localhost:8105/metrics | grep -E '^catalog_'

# follow one failed request through both services: logs, then spans (needs jq)
sleep 2   # spans are batched for up to 1s
T=$(jq -r 'select(.msg == "catalog call failed") | .trace_id' /tmp/gateway.out | head -1)
cat /tmp/gateway.out /tmp/catalog.out | jq -c "select(.trace_id == \"$T\") | del(.time)"
cat /tmp/gateway.out /tmp/catalog.out | jq -r "select(.SpanContext.TraceID == \"$T\") | [.Resource[0].Value.Value, .Name, .SpanContext.SpanID, .Parent.SpanID, .Status.Code] | @tsv" | column -t -s$'\t'

# graceful shutdown: readiness fails first, liveness stays up, then the listener closes
kill -TERM $GW; sleep 0.3
curl -s -o /dev/null -w 'readyz  %{http_code}\n' localhost:8104/readyz
curl -s -o /dev/null -w 'healthz %{http_code}\n' localhost:8104/healthz
sleep 4
curl -s -o /dev/null -w 'after the drain: %{http_code}\n' localhost:8104/healthz
kill -TERM $CAT
```

Output of that run (the failures are random, so your counts and IDs differ). The 4 junk paths
became one `unmatched` series; 20 + 12 requests to five different ids share one route label;
every `trace_id` below is the same, in both services; and each span's parent is the span above
it in the chain (the catalog's server span hangs off the gateway's `HTTP GET` client span):

```
{"id":"p-1","name":"Espresso mug","price_cents":1250}
{"error":"catalog unavailable"}
{"error":"catalog unavailable"}
http_requests_in_flight 0
http_requests_total{code="200",method="GET",route="/products/{id}"} 20
http_requests_total{code="404",method="GET",route="unmatched"} 4
http_requests_total{code="502",method="GET",route="/products"} 1
http_requests_total{code="502",method="GET",route="/products/{id}"} 12
catalog_products 4
catalog_repo_reads_total 33
{"level":"ERROR","msg":"catalog call failed","service":"gateway","err":"catalog returned 500 Internal Server Error","trace_id":"2c90239f0b7378796d5f91dbfee899de","span_id":"ae449e07df2a530c"}
{"level":"ERROR","msg":"request","service":"gateway","method":"GET","path":"/products/nope","route":"/products/{id}","status":502,"duration_ms":0.283,"trace_id":"2c90239f0b7378796d5f91dbfee899de","span_id":"ae449e07df2a530c"}
{"level":"WARN","msg":"get product failed","service":"catalog","id":"nope","err":"repository unavailable","trace_id":"2c90239f0b7378796d5f91dbfee899de","span_id":"deddd314c96166cf"}
{"level":"ERROR","msg":"request","service":"catalog","method":"GET","path":"/products/nope","route":"/products/{id}","status":500,"duration_ms":0.045,"trace_id":"2c90239f0b7378796d5f91dbfee899de","span_id":"f6acadc4b484830f"}
gateway  HTTP GET            9d6a07c6f0fa5953  ae449e07df2a530c  Error
gateway  GET /products/{id}  ae449e07df2a530c  0000000000000000  Error
catalog  repo.Get            746542feaae288c9  deddd314c96166cf  Error
catalog  catalog.Get         deddd314c96166cf  f6acadc4b484830f  Error
catalog  GET /products/{id}  f6acadc4b484830f  9d6a07c6f0fa5953  Error
readyz  503
healthz 200
after the drain: 000
```

Spans are printed by `stdouttrace` as one long JSON line each (they share stdout with the
log lines, hence the `jq` filters above): `select(.trace_id)` picks logs, `select(.SpanContext)`
picks spans. `gateway -sample 0.5` records half of the traces; the catalog never starts one,
it follows the gateway's decision.

### With Docker (optional)

```sh
docker compose up -d                      # Prometheus :9090, Jaeger UI :16686, OTLP/HTTP :4318
export OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318
/tmp/obsapi-bin/catalog -fail-rate 0.3 & CAT=$!   # same binaries as above; spans now go to Jaeger
/tmp/obsapi-bin/gateway & GW=$!
curl -s localhost:8104/products/p-1       # then search service "gateway" in Jaeger
# Prometheus: sum by (route, code) (rate(http_requests_total[1m]))
kill -TERM $GW $CAT; docker compose down -v
```

The services run on the host; Prometheus reaches them through `host.docker.internal`
(`prometheus.yml`). The Jaeger image is `latest` — pin it once you have pulled a version you like.

## Rewrite exercises

1. Rewrite `Obs.Handler` and `Obs.instrument` (`internal/obs/http.go`) from memory. Then put
   a middleware that calls `r.WithContext` between `instrument` and the mux and see what the
   `route` label becomes.
2. Write your own `SpanExporter` (two methods) that prints one line per span, and select it in
   `NewExporter` — the provider, sampler and tests should not need to change.
3. Attach the trace ID to histogram observations as an exemplar (`ObserveWithExemplar`,
   served as OpenMetrics) so a slow bucket links to a trace.
4. Make the gateway's `/readyz` depend on the catalog's `/healthz`, and map client timeouts to
   504 instead of 502. Test both with the catalog stopped and with a slow catalog.
5. Add simulated latency to `MemRepo` (a random sleep up to N ms honouring `ctx`) and a
   `-latency` flag. Watch the buckets in `metrics.go` earn their keep, then cancel the client
   request mid-flight and read what the spans say.

## Deliberately left out

Histogram exemplars, OpenTelemetry *metrics* (Prometheus owns metrics here; otelhttp's own
instruments go to the default no-op meter), outbound-call metrics, panic recovery in
`instrument`, request-ID headers, auth, and any real database — `MemRepo` is in-memory.

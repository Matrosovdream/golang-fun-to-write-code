# grpc-inventory — gRPC microservices pair

Two services: **inventory** (products, stock, live stock feed) and **orders** (places orders by
calling inventory over gRPC), plus a demo client. All four RPC shapes in one proto.

## What this project teaches

- **API-first with protobuf**: the contract in `proto/` is the source of truth; `buf generate`
  produces `gen/` (checked in so the repo builds without tooling). `buf` compiles protos in
  pure Go — no system `protoc` needed, only `protoc-gen-go`/`protoc-gen-go-grpc` in PATH.
- **All four RPC shapes** and their idioms:
  - unary (`GetProduct`, `ReserveStock`) — status codes are the error contract (`codes.NotFound`);
  - client streaming (`BulkAddProducts`) — `Recv` until `io.EOF`, then `SendAndClose`;
  - server streaming (`WatchStock`) — subscription bound to `stream.Context()`;
  - bidirectional (`Reserve`) — request/response pairs over one stream.
- **Interceptors** — gRPC middleware: chained logging + panic recovery, unary and stream
  variants, and why recovery goes outermost.
- **Metadata**: `x-request-id` flowing from orders into inventory's logs.
- **Deadline propagation**: orders caps downstream calls with `context.WithTimeout`; the
  caller's deadline rides along automatically.
- **Circuit breaker** (hand-rolled, ~50 lines): closed → open → half-open, and *why* failing
  fast beats queueing on a dead dependency. Production: `sony/gobreaker`.
- **Service extras**: health service (k8s probes), reflection (grpcurl), `GracefulStop`.
- **bufconn testing**: the entire gRPC stack over an in-memory listener — fast, no ports.

## Run

```sh
go test -race ./...

go run ./cmd/inventory              # terminal 1
go run ./cmd/orders                 # terminal 2
go run ./cmd/client                 # terminal 3 — exercises everything

# poke it interactively (reflection is on):
grpcurl -plaintext localhost:9091 list
```

Regenerate after editing the proto: `make proto` (needs `go install`ed buf + plugins).

## Rewrite exercises

1. Write the proto from scratch for a different domain (library/loans) and implement both sides.
2. Add TLS: generate a self-signed cert, replace `insecure.NewCredentials()` on both ends.
3. Add a retry interceptor on the client for `codes.Unavailable` (careful: only idempotent RPCs).
4. Kill inventory while orders is under load and watch the breaker open, then half-open probe.
5. Add pagination to a new `ListProducts` unary RPC (page token pattern, not offset).

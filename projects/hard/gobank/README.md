# gobank — banking REST API (the production-grade reference)

Users, accounts, deposits, transfers, history — the "how a real Go backend looks" project.

## What this project teaches

- **Clean architecture in Go terms**: `domain` (pure types + errors) ← `repo` (interfaces)
  ← `repo/postgres` (SQL) ← `service` (business rules) ← `transport/http` (chi handlers).
  Dependencies only point inward; `main.go` is the composition root that wires everything.
- **Unit of work over context**: `TxManager.WithinTx` puts an `*sqlx.Tx` into the context;
  every repo query goes through `q(ctx)` and transparently joins the transaction.
  This is the idiomatic answer to "how do services compose repo calls atomically?"
- **Correct concurrent transfers**: `SELECT ... FOR UPDATE` row locks, taken in **ascending
  id order** so A→B and B→A can't deadlock; balance checks inside the lock; a `CHECK
  (balance >= 0)` constraint as the last line of defense. The integration test hammers
  opposite transfers concurrently and asserts the ledger.
- **Money as int64 minor units** — never float.
- **sqlx + pgx**: `GetContext`/`SelectContext` with struct tags, translating driver errors
  (`23505 unique_violation`) into domain errors at the repo boundary.
- **golang-migrate with embedded migrations** (`embed.FS`) — the binary carries its schema.
- **Auth done properly**: bcrypt (with the dummy-compare trick against user enumeration),
  JWT HS256 via `golang-jwt/v5` with `WithValidMethods`, auth middleware with an unexported
  context key.
- **zap** structured logging (dev vs prod configs), **viper** config file + `GOBANK_*` env
  overrides, **validator** DTO tags (including `iso4217` currency codes).
- **testcontainers-go**: integration tests against real PostgreSQL, no mocks.
- **Docker**: multi-stage build (~15MB image), compose with healthcheck, Makefile targets.

## Run

```sh
make up      # start postgres (docker compose)
make run     # migrate + serve on :8085

# in another terminal:
curl -s -X POST localhost:8085/api/register -d '{"email":"a@b.co","password":"hunter2hunter2"}'
TOKEN=...    # from the response
curl -s -X POST localhost:8085/api/accounts -H "Authorization: Bearer $TOKEN" -d '{"currency":"USD"}'
curl -s -X POST localhost:8085/api/accounts/1/deposit -H "Authorization: Bearer $TOKEN" -d '{"amount":10000}'
curl -s localhost:8085/api/accounts -H "Authorization: Bearer $TOKEN"

make test    # unit tests (fast, no Docker)
make itest   # + testcontainers integration suite
make down
```

## Rewrite exercises

1. Rebuild the layers bottom-up: domain → repo interfaces → postgres → services → handlers.
2. Remove the ordered locking (lock `from` then `to` as given) and run the concurrent
   integration test repeatedly — watch Postgres detect deadlocks. Put the ordering back.
3. Add idempotency keys to `POST /api/transfers` (unique header + table) so retries are safe.
4. Add pagination to history (`?after_id=&limit=`), keeping it index-friendly.
5. Swap zap for slog behind a tiny interface of your own — feel where the layers rub.

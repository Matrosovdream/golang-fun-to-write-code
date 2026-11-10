# orderflow — event-driven order pipeline: outbox, sagas, idempotent consumers

Three services (`orders`, `inventory`, `payments`) that talk only through NATS JetStream
(embedded, in-process), one SQLite file each. An order is a saga: reserve stock → charge →
confirm, with compensation when a step fails. The reliability layer — transactional outbox,
inbox, retries, dead-letter subject — is written by hand, and chaos tests break it on purpose.

## What this project teaches

The one idea: **there are no distributed transactions.** A database and a broker cannot commit
together, so you stop trying. You get *at-least-once* delivery (the outbox may publish twice,
JetStream redelivers until acked) and make every consumer *idempotent*; the combination is
"effectively once". Everything below is one half of that sentence.

- **Transactional outbox** — `internal/outbox`. `outbox.Insert(ctx, tx, ev)` takes the
  `*sql.Tx`, so the event row is written in the same transaction as the business row and
  there is no API to publish without one. `Relay.Flush` publishes unsent rows in order and
  stops at the first failure so nothing overtakes it. The relay sets the event ID as
  `Nats-Msg-Id`, so JetStream drops a republished duplicate — but the dedupe window is
  finite (2 minutes by default), so **no consumer relies on it**. The tests prove both
  layers: `noDedupe` in `chaos/harness_test.go` strips the header (a broker that already
  forgot), `TestBrokerDedupeAbsorbsRelayRepublish` keeps it.
- **Inbox / idempotency key** — `inbox.Consumer.apply`. One transaction: `INSERT INTO inbox`
  keyed by event ID (conflict → duplicate → skip), run the handler, commit; ack only
  afterwards. Crash before the commit: nothing recorded, redelivery re-runs the handler.
  Crash between commit and ack: redelivery hits the inbox row. A handler error rolls back
  the inbox row too (`TestFailedHandlerRollsBackEverythingAndIsRetried`).
- **Retry, backoff, dead letters** — `Consumer.fail`: NAK with exponential delay, and on the
  last allowed delivery publish the message plus `Failure-Reason` / `Original-Subject` /
  `Deliveries` headers to `dlq.<stream>` and ack the original, so a poison message cannot
  wedge the stream (`TestPoisonMessagesGoToTheDLQAndTheStreamKeepsFlowing`).
- **Saga as an explicit state machine** — `orders/saga.go`. A `transitions` table says which
  moves are legal; a `reactions` table says which reply moves the order and which command
  goes out next, in the same transaction. Illegal moves are errors
  (`TestTransitionsEnumerated` walks all 36 state pairs, `TestEveryReplyInEveryState` all
  36 state × reply pairs). Compensation is a path, not a rollback:
  `STOCK_RESERVED → CANCELLING → CANCELLED` releases the stock a declined payment held.
- **Orchestration vs choreography.** Here `orders` is the orchestrator: it alone knows the
  flow and inventory/payments just answer commands, so the whole saga reads in one table and
  a new step is a new row. In a choreographed saga each service would react to the others'
  events (payments listens for `stock_reserved`, inventory listens for `payment_declined`);
  there is no single point of coupling, but the flow exists only in the heads of five
  subscriptions and a stuck order has no owner. Exercise 4 turns this one into the other.
- **Ordering.** The orchestrator sends one command per order and waits for the reply before
  sending the next, so the messages of one order are causally ordered by construction and
  only *different* orders interleave. That is why subjects are not keyed by order ID and
  consumers need no sequence numbers. Where you cannot get that property, add a version
  column checked in the handler (an illegal transition is exactly that check), or
  `MaxAckPending: 1` per partition.
- **SQLite without `SQLITE_BUSY`** — `sqlite.Open`: WAL (readers never block), `busy_timeout`
  (a second writer waits), `_txlock=immediate` (the lock is taken at `BEGIN`, so a
  transaction cannot fail while upgrading). `sqlite.InTx` keeps transactions short; none
  spans a network call.
- **Stock cannot go negative** — `inventory.reserve` is one conditional `UPDATE ... WHERE
  available >= ?`, and `CHECK` constraints make a negative bucket impossible even through a
  bug. Running out is a *rejection reply*, not an error.
- **Fault injection as plain function fields** — `Relay.AfterPublish`,
  `Consumer.BeforeCommit`, `Consumer.BeforeAck`, `payments.Service.Decline`. No mocking
  framework: a test assigns a closure.
- **Chaos tests** — `internal/chaos`: real embedded broker per test, seeded faults, and
  `check.Invariants` (the same function `placeorders` runs): stock never negative, every order
  terminal, nothing left reserved, confirmed quantity + remaining stock == initial stock.
  Waiting is `require.Eventually` with 90 s bounds that only bound a hang, never a sleep.
- **Embedded NATS** — `bus.Start`: `nats-server` in-process with JetStream, random port,
  `StoreDir` on disk.

Messages (the event type is the NATS subject; first token picks the stream):

| from → to | event | stream |
|---|---|---|
| orders → inventory | `inventory.reserve`, `inventory.commit`, `inventory.release` | `inventory` |
| orders → payments | `payments.charge` | `payments` |
| inventory → orders | `orders.stock_reserved`, `orders.stock_rejected`, `orders.stock_committed`, `orders.stock_released` | `orders` |
| payments → orders | `orders.payment_succeeded`, `orders.payment_declined` | `orders` |
| any consumer → DLQ | the poison message + reason | `dlq` |

Every event is an envelope: `id`, `type`, `aggregate_id` (the order), `correlation_id` (one per
saga, taken from `X-Correlation-ID` or generated), `time`, `data`.

### Going to Postgres and several relays (not implemented)

Swap `modernc.org/sqlite` for `pgx`: `ON CONFLICT DO NOTHING` and `RETURNING` work unchanged,
`BEGIN IMMEDIATE` and the pragmas disappear. A single relay here is correct because SQLite
serialises writers, so `seq` order is commit order. To run several relays on Postgres, claim
rows with `SELECT ... WHERE sent_at IS NULL ORDER BY seq LIMIT n FOR UPDATE SKIP LOCKED` inside
a transaction that also marks them sent. Two things change: relays no longer see rows in global
order (partition by aggregate ID if per-order order matters, or accept it, as this project
does), and a sequence can commit out of order, so a relay must select on `sent_at IS NULL`
exactly as this one does, never on "seq greater than the last one I saw".

## Run

```sh
go test -race ./...
go test -race -count=5 ./internal/chaos
CHAOS_SEED=1794279840076787000 go test -race -v -run TestChaosMany ./internal/chaos

go run ./cmd/orderflow                      # terminal 1: all three services + NATS, REST on :8107
go run ./cmd/placeorders -n 20              # terminal 2: place orders, wait, print table, check invariants

curl -s -X POST localhost:8107/orders -d '{"sku":"widget","qty":2,"amount":200}'
curl -s localhost:8107/orders/ord_...       # state plus the full history
curl -s localhost:8107/stock

go run ./cmd/orderflow -payment-fail-rate 0.5 -debug -data-dir ./data   # more cancellations, every message logged, state kept across restarts
```

Real output of `placeorders -n 20` against a fresh server (stock: widget 12, gadget 8, gizmo 5;
20% of charges declined; the first line is the order from the `curl` above):

```
ORDER                 SKU     QTY  STATE      REASON
ord_b037a502c3a963f0  widget  2    CONFIRMED  
ord_620e821d56b6b8c1  widget  1    CANCELLED  card declined
ord_d6fbe11a3fd26c57  widget  3    CONFIRMED  
ord_ac9f8ddc97e4079d  gadget  3    CONFIRMED  
ord_ecfb31df81ac3856  widget  1    CONFIRMED  
ord_92c33a1769563d5b  gizmo   1    CONFIRMED  
ord_96c0fb556755f070  gizmo   3    CONFIRMED  
ord_1bf48b1a29f146d2  gizmo   2    CANCELLED  insufficient stock for gizmo
ord_a44c33e096795d99  gadget  3    CONFIRMED  
ord_433eae90c2938c86  gadget  1    CONFIRMED  
ord_0fe3ec9585d94281  gizmo   1    CONFIRMED  
ord_ac4af4fcc3fbce78  gadget  2    CANCELLED  insufficient stock for gadget
ord_243efcd1165e91d5  widget  1    CONFIRMED  
ord_2405258eba9e18e4  gizmo   3    CANCELLED  insufficient stock for gizmo
ord_71fb3ab64e662cac  widget  1    CONFIRMED  
ord_10723ba96b601a70  gizmo   1    CANCELLED  insufficient stock for gizmo
ord_16ba486fc81f8e95  widget  1    CONFIRMED  
ord_77ea19e0c8d76b40  gadget  1    CONFIRMED  
ord_f28c82169aa0757b  gizmo   3    CANCELLED  insufficient stock for gizmo
ord_50d376b543bc01f1  gizmo   1    CANCELLED  insufficient stock for gizmo
ord_e0aac962a157be22  gizmo   1    CANCELLED  insufficient stock for gizmo

SKU     INITIAL  AVAILABLE  RESERVED  SOLD
gadget  8        0          0         8
gizmo   5        0          0         5
widget  12       3          0         9

21 orders: 13 confirmed, 8 cancelled
invariants: OK (stock never negative, every order terminal, nothing reserved, confirmed + available == initial)
```

The second order was declined: it went `PENDING → STOCK_RESERVED → CANCELLING → CANCELLED`, and
its widget was released again (12 − 9 sold = 3 available, 0 reserved). A single order, as the
`curl` showed it:

```
{"id":"ord_b037a502c3a963f0","sku":"widget","qty":2,"amount":200,"state":"CONFIRMED","history":["PENDING","STOCK_RESERVED","PAID","CONFIRMED"]}
```

The chaos test logs what it injected (one run; `-v` shows it): 100 orders against 90 units,

```
seed=1794279840076787000 (rerun with CHAOS_SEED=1794279840076787000)
confirmed=47 declined=14 out-of-stock=39 faults: relay=56 before-commit=26 before-ack=55 duplicates=109
```

The seed fixes the orders and the sequence of fault rolls, but goroutine scheduling decides which
event meets which roll, so a rerun of the same seed gives different counts (`confirmed=32` the
second time). It is a way to replay the *kind* of run, not a deterministic replay; the invariants
have to hold in every interleaving, which is the point.

## Rewrite exercises

1. Rewrite `inbox.Consumer.apply` and `fail` from the diagram in the package comment, then run
   `go test -race ./internal/inbox ./internal/chaos` against your version.
2. Break it on purpose and predict which test goes red before you run it: drop the inbox
   check, move `ack` before the commit, remove `ON CONFLICT DO NOTHING`, make the relay mark
   rows sent *before* publishing. Which of these does only the chaos suite catch?
3. Run two relays on the same SQLite file and watch duplicates and reordering appear. Fix it
   with a claim column (`UPDATE ... SET claimed_by = ? WHERE ... RETURNING`), the SQLite
   stand-in for `SKIP LOCKED`.
4. Turn the orchestration into choreography: payments consumes `inventory`'s
   `stock_reserved` directly, inventory consumes `payment_declined` to release. Which tests
   stop making sense, and which invariant is now hardest to state?
5. Add a refund compensation: `PAID` can fail to confirm (inject it in `inventory.commit`),
   which needs `REFUNDING`, a `payments.refund` command and new rows in both saga tables.
   Extend `TestEveryReplyInEveryState` and add a chaos case.

## Deliberately left out

- **Saga timeouts.** A lost reply (a service down for good) leaves an order in its state
  forever; a real orchestrator needs a deadline per step and a sweeper.
- **Cleanup.** Sent outbox rows and inbox rows are never deleted; the inbox needs a retention
  window longer than the longest possible redelivery.
- **Several relays / Postgres** — described above, not built.
- **DLQ replay tooling.** Dead letters sit in the `dlq` stream; inspect them with the
  `nats` CLI or `Stream.GetMsg`.
- One SKU per order, no prices, no auth, no REST idempotency key, no metrics or tracing
  (the correlation ID is carried in every event and logged in `-debug` mode, nothing more).

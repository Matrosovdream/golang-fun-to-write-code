# crdtpad — collaborative text editing with a sequence CRDT

A server and a tiny embedded web client where several people edit one document at once.
There is no central ordering of edits: every replica applies its own edits instantly, and
all replicas converge to the same text whatever order the edits arrive in. Offline edits
merge on reconnect. The document is an **RGA** (Replicated Growable Array).

## What this project teaches

- **The RGA insertion rule** — `internal/rga/doc.go`, `integrate`. Put the new character
  right after its `after` character, then keep moving right past every node with a
  *greater* ID. Four lines, and the entire convergence argument (below) hangs on them.
- **Lamport clocks as unique, ordered IDs** — `internal/rga/op.go`. A character's ID is
  `(clock, replica)`, compared lexicographically; `Doc.Insert` uses `clock = highest seen + 1`,
  `integrate` folds in every clock it sees.
- **Tombstones** — a delete only sets `dead`. The node stays in the list and in the ID
  index, so an insert "after" a character someone deleted concurrently still finds its parent
  (`TestConcurrentDeleteAndInsertAfterDeleted`).
- **Commutative and idempotent ops** — `Doc.Apply`/`drain`. An op is applied when it is
  next in its replica's sequence *and* the node it refers to exists; otherwise it waits in the
  pending buffer. Re-delivery is a no-op (`Seq <= version[replica]`).
- **Version vectors that mean something** — every op also has a per-replica `Seq`
  (1, 2, 3, … with no gaps) next to the Lamport clock. Because ops of one replica are applied
  strictly in `Seq` order, `version[r] = n` says "I have all of r's ops up to n", and
  `Doc.Missing(since)` is a one-line filter over the applied log.
- **Testing a distributed algorithm with randomized interleavings** —
  `internal/rga/converge_test.go`. Seeded, replayable (`seed=17` in the subtest name and a
  replay command printed on failure), plus a fuzz target over the same seeds. The checks are
  the three things a CRDT promises: replicas agree, no character is lost or resurrected, and
  every insert still sits between the neighbours its author saw.
- **A per-document hub goroutine** — `internal/server/hub.go`. As in `chathub`, the
  document, oplog and peer set belong to one goroutine; connection goroutines talk to it over
  channels. `trySend` never blocks: a slow peer is dropped, and its next `hello` version makes
  the server resend exactly what it missed. `text()` shows request/response over a channel.
- **Op-log persistence** — `internal/server/oplog.go`. One JSON object per line in
  application order (a valid causal order); replay through `Doc.Apply` rebuilds the
  document. The hub stops rather than serve a document it can no longer persist.
- **A browser replica in 124 lines of JS** — `web/rga.js` is the same algorithm over an
  array; `web/index.html` turns textarea edits into ops with a common-prefix/suffix diff and
  keeps the caret by remembering it as "just after character X" (an ID, which survives every
  remote edit).

### Design: why concurrent inserts converge

Think of the characters as a tree: each insert is a child of its `after` character. The
text is a pre-order walk of that tree with **children ordered by descending ID**. A Lamport
clock guarantees a node's ID is greater than its parent's, so every descendant has a greater
ID than all of its ancestors. That is why "skip right past greater nodes" is correct: it
steps over whole subtrees of greater siblings and stops at the first smaller sibling, or at
the end of the parent's subtree, which is exactly where the tree says the new node belongs.
The resulting list depends only on the *set* of ops applied, never their order.

Worked example (`TestConcurrentInsertsAtOneSpot`): three replicas share `ab`, and each types
one character between `a` and `b` without hearing from the others. All three inserts get
clock 3, so replica name breaks the tie: `(3,C) > (3,B) > (3,A)`. Greater goes leftmost, so
every replica ends with `aZYXb`, in all six delivery orders.

Two people typing a word each at the same spot do not interleave
(`TestConcurrentRunsDoNotInterleave`: `abc` vs `xyz` gives `xyzabc`): each word is a chain of
children, and the chains hang off the first letters, which are siblings.

The wire protocol (`internal/wire`, JSON over WebSocket):

| direction | message | meaning |
|---|---|---|
| client → server | `hello {replica, version}` | first message; "this is what I have" |
| server → client | `sync {version, ops}` | the ops the client lacks, plus the server's version |
| either way | `ops {ops}` | live edits; after `sync` the client also sends what the server's version says it lacks (its offline edits) |

The server rebroadcasts every op it newly applied, including to the sender: a duplicate is a
no-op there, and an op released from the server's pending buffer may be news to the sender.

### CRDT vs operational transformation

OT ops are positional ("insert at 5"), so two concurrent ops must be *transformed* against
each other, and the transform functions are famously hard to get right without a central
server imposing one order. RGA ops name characters by ID instead of position, so they commute
by construction: no transformation, no central sequencer, and the server here is just a relay
with a log. The price is metadata: an ID and a tombstone per character, forever.

### coder/websocket vs gorilla/websocket (used in `chathub`)

- **Context everywhere.** `Read`/`Write` take a `context.Context`; cancelling it closes the
  connection. `serveWS` therefore has one teardown mechanism (`cancel`) that the hub, the
  writer and the reader can all trigger. gorilla uses `SetReadDeadline`/`SetWriteDeadline`
  plus a pong handler that pushes the deadline.
- **No pump boilerplate for writes.** `Conn.Write` is safe to call concurrently (gorilla
  requires exactly one writer goroutine, hence `chathub`'s `writePump`). Reads still need a
  single reader. The per-peer writer goroutine here exists for another reason: so the hub
  can hand a message to a bounded channel without waiting on a socket.
- **Defaults differ.** `Accept` checks the `Origin` header against `Host` unless told
  otherwise; the read limit defaults to 32 KiB (the client here raises it because a `sync` can
  carry a whole document; gorilla has no default limit).
- **`wsjson`** reads and writes JSON messages in one call; with gorilla it is
  `ReadJSON`/`WriteJSON`, the same idea.
- **Cost of the ctx model:** any error, including an expired context, closes the connection,
  so a per-write timeout is not a "try again" tool.

## Run

```sh
go test -race ./...
go test -run xxx -fuzz=FuzzConvergence -fuzztime=20s ./internal/rga
go test -run 'TestConvergence/seed=17$' -v ./internal/rga     # replay one seed

go run ./cmd/crdtpad                          # terminal 1: http://localhost:8106, oplogs in ./data
go run ./cmd/crdtbot -bots 4 -duration 5s     # terminal 2: the demo
curl -s localhost:8106/doc/pad                # the server's text for document "pad"
```

Open `http://localhost:8106/` in two tabs (`?doc=name` picks another document), type in
both, press "Go offline" in one, type in both, press "Go online". `crdtbot -doc pad` against
the same document adds bots to the mix; stop typing when they finish if you want its final
"converged" check to be exact.

Demo output (`crdtbot` against a fresh document, run on this machine):

```
4 bots editing "bots-1794279468" on http://127.0.0.1:8106 for 5s
  bot0-577d  ops=422  offline episodes=11  chars=632  matches server: true
  bot1-0b87  ops=427  offline episodes=10  chars=632  matches server: true
  bot2-1bbc  ops=385  offline episodes=5  chars=632  matches server: true
  bot3-80e0  ops=399  offline episodes=8  chars=632  matches server: true
1633 ops from 4 bots (34 offline episodes)
converged: true  (server holds 632 characters)
document starts: "iahvnsrojrjwyrgiscohctifuqonrmogmdtm ttoc cfbbnuwspzsavkrifgwsnwanecpr..."
```

Each bot edits at random positions every 5–25 ms and now and then drops its connection,
makes ten more edits blind, and reconnects; "converged" compares every bot's text with the
server's after all ops have been delivered. Kill the server, start it again, and
`curl localhost:8106/doc/<name>` returns the same text: the oplog is replayed on start.

The browser page was exercised in a real browser: headless Google Chrome driven over the
DevTools protocol (a throwaway script, not part of this repo) typed into two tabs, checked the
caret and selection after remote inserts and deletes, a surrogate-pair character, an offline
edit that merged on reconnect, and two tabs typing while three `crdtbot` bots edited the same
document. It has not been tried in Firefox or Safari.

## Rewrite exercises

1. Rewrite `Doc.integrate` and `Doc.Apply`/`drain` from memory, then your own `simulate`.
   Break the skip comparison on purpose and watch which tests go red (`TestConvergence`
   names the seed and the character that moved).
2. Replace the linked list + ID map with a balanced tree (or a skip list) keyed by visible
   position, so `Insert`/`Delete` by position stop being O(n).
3. Add tombstone garbage collection: a tombstone can be dropped once every replica's version
   covers its delete and nothing can still name it as `after`. What does the server need to
   track per replica, and what happens to a replica that is offline for a month?
4. Make the sync incremental: send the catch-up in chunks instead of one message, and add a
   snapshot (text + version + IDs) to the oplog so restarts do not replay every op.
5. Port the cursor idea to the server: broadcast each client's caret as an ID anchor so
   others can draw it. Which of the existing messages would carry it?

## Deliberately left out

- **Tombstone GC** and oplog compaction (exercise 3/4): memory and the oplog only grow.
- Rich text, formatting, shared cursors/presence (characters only).
- **Replica identity safety:** a replica name must never be reused with fresh state, or its
  `Seq`s collide with the old ones and the new ops are discarded as duplicates. The browser
  and bots draw a random name per start; nothing in the server enforces it. Ops are only
  validated for shape (and `Clock` above the parent's), not authenticated.
- **Limits and auth:** no cap on the pending buffer, document count or document size, no
  authentication; `GET /doc/{name}` creates the document on first touch.
- **Idle hubs** are never evicted; app-level WebSocket pings are omitted (Go enables TCP
  keepalive on accepted connections; a dead peer is dropped when its buffer fills).
- A torn last oplog line (power loss) makes the next start fail loudly instead of repairing
  the file; appends are not fsynced.
- The caret diff in `web/index.html` is a plain common-prefix/suffix diff: typing a character
  next to an identical one may be attributed to the other end of the run. The text is
  the same; only a concurrent edit inside that run could land differently. Input-method
  composition is not special-cased.

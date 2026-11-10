# raftkv — replicated key-value store on Raft consensus

A 3–5 node cluster: leader election, log replication, durable state, and an HTTP KV API
(`PUT/GET/DELETE /kv/{key}`) whose followers redirect to the leader. Built so the hard part,
*testing a distributed system*, is deterministic: a fake clock and a fault-injecting in-memory
network run whole partition/crash scenarios in milliseconds.

## What this project teaches

**The design in a few lines.** One event-loop goroutine per node (`Node.Run`,
`internal/raft/node.go`) owns all Raft state; peer messages, client proposals, status queries and
clock ticks arrive on channels, so the core has no mutexes (the `chathub` lesson, with `-race` as
the proof). Time comes from an injectable `clock.Clock`, the network from a `Transport`, the disk
from a `Storage`. Read the **Figure 2 design comment at the top of `node.go`** first: it lists the
paper's state and rules and names the function that enforces each one.

- **Terms and elections**: tick-based randomized election timeouts (`resetElectionTimer`), any
  higher term demotes (`step` → `becomeFollower`), the voting restriction that guarantees a leader
  holds every committed entry (`onRequestVote`, `upToDate`), votes counted in a set so a duplicated
  response cannot vote twice (`onVoteResp`).
- **Persist before you reply**: `saveHardState` runs before a vote is granted or a
  `RequestVote` leaves; entries hit `Storage` before an ack. `Storage` has `MemStorage` and
  `FileStorage` (`filestorage.go`: JSON-lines write-ahead log, one fsync per call, a torn last
  record from a crash is cut off on load). A "crash" in tests is "drop the `Node`, keep its `Storage`".
- **Log matching**: the `prevLogIndex/prevLogTerm` check and conflict truncation *only on a real
  conflict* (`onAppendEntries`; a delayed duplicate must never shorten a log), `nextIndex`
  back-off using the follower's hint, `matchIndex` that only moves forward (`onAppendResp`).
- **Figure 8**: `advanceCommit` commits by counting replicas only for entries of the current term;
  a new leader appends a no-op (`becomeLeader`) so older entries commit behind it.
- **Messages, not RPCs**: `types.go` has one flat `Message` for the paper's four message kinds. A
  node never blocks on a peer, and the network can drop, delay, duplicate and reorder either
  direction independently.
- **Exactly-once apply and client sessions**: `applyCommitted` applies in order once per node
  incarnation. `ErrNotLeader` or a timeout does *not* mean "not committed", so requests carry
  `(clientID, seq)` and `kv.Store.Apply` answers a retry from its session table instead of
  re-running it.
- **A deposed leader never acks an overwritten write**: `becomeFollower` fails every pending
  waiter with `ErrNotLeader` (`failWaiters`); proposals wait on a per-log-index channel.
- **Linearizable reads through the log**: `kv.Exec` sends gets through consensus like writes.
- **Deterministic distributed tests** (`internal/raft/*_test.go`):
  - `clock.Fake` moves only when the test advances it; `synctest.Wait` then returns when every
    node and client goroutine has finished reacting. No sleeps anywhere.
  - `transport.Network`: per-link drop / duplicate / delay+jitter on the fake clock, partitions,
    isolate, heal, detach (crash). Per-link seeded RNGs.
  - `cluster_test.go`: the harness. `leaderTracker` is the "at most one leader per term"
    invariant, fed by every node's `OnRole` callback, so it watches every test continuously;
    `checkApplied` is state-machine safety (no two nodes ever apply different commands at one index).
  - `node_test.go`: Figure 2 rules one at a time against a single node played by hand
    (vote rules table, vote survives restart, log matching, truncation, Figure 8, deposed leader).
  - `scenario_test.go`: healthy 3- and 5-node election, leader crash, minority partition cannot
    commit while the majority can, heal overwrites the stale leader's entries and logs converge,
    whole-cluster restart from file storage, retry dedupe across a leader change.
  - `stress_test.go` + `lincheck_test.go`: 4 concurrent clients retrying forever against 5 nodes
    under 10% drop / 5% duplicate / jitter while a nemesis partitions, isolates, crashes and
    restarts nodes; afterwards every key's history is checked for linearizability (a Wing & Gong
    search) and all nodes must agree. The checker itself is tested against known-bad histories.
- **HTTP and the real thing**: `transport.HTTP` keeps a bounded queue and one sender goroutine per
  peer (a dead peer costs dropped messages, not goroutines); `server.go` redirects with 307 so the
  method and body survive; `cmd/raftkv` wires it with `signal.NotifyContext` and a clean stop order.

## Run

```sh
go test -race ./...
go test -race -count=5 ./...

# the stress test prints its seed; replay one with
RAFTKV_SEED=2 go test -race -v -run TestStress ./internal/raft

# a real 3-process cluster on 127.0.0.1:8201-8203 (state in .cluster/)
scripts/cluster.sh up
sleep 2
for i in 1 2 3; do curl -s localhost:820$i/status; done      # who leads?
curl -si -X PUT -d hello localhost:8202/kv/greeting          # a follower: 307 to the leader
curl -sL -X PUT -d hello localhost:8202/kv/greeting          # -L follows it, body included
curl -sL localhost:8202/kv/greeting
scripts/cluster.sh kill 1                                    # SIGKILL: crash the leader
sleep 2.5
for i in 2 3; do curl -s localhost:820$i/status; done        # a new leader, a higher term
curl -sL localhost:8202/kv/greeting                          # data survived
scripts/cluster.sh start 1                                   # it recovers from its WAL...
sleep 2
for i in 1 2 3; do curl -s localhost:820$i/status; done      # ...and catches up
scripts/cluster.sh down
```

Requests may carry `X-Client-ID` and `X-Seq`; resending the same pair after a 504 or a dropped
connection is applied at most once.

### What the demo printed

Node 1 happened to win term 1 (`kill` below is `kill -9`). Output of the commands above, in order:

```text
$ for i in 1 2 3; do curl -s localhost:820$i/status; done
{"id":1,"term":1,"role":"leader","leader":1,"lastIndex":1,"commitIndex":1,"lastApplied":1}
{"id":2,"term":1,"role":"follower","leader":1,"lastIndex":1,"commitIndex":1,"lastApplied":1}
{"id":3,"term":1,"role":"follower","leader":1,"lastIndex":1,"commitIndex":1,"lastApplied":1}

$ curl -si -X PUT -d hello localhost:8202/kv/greeting
HTTP/1.1 307 Temporary Redirect
Location: http://127.0.0.1:8201/kv/greeting
Content-Length: 0

$ curl -sL -X PUT -d hello localhost:8202/kv/greeting -w '%{http_code} via %{url_effective}\n' -o /dev/null
204 via http://127.0.0.1:8201/kv/greeting
$ curl -sL localhost:8202/kv/greeting
hello

$ scripts/cluster.sh kill 1
$ curl -s localhost:8202/status
{"id":2,"term":2,"role":"follower","leader":3,"lastIndex":5,"commitIndex":5,"lastApplied":5}
$ curl -s localhost:8203/status
{"id":3,"term":2,"role":"leader","leader":3,"lastIndex":5,"commitIndex":5,"lastApplied":5}
$ curl -sL localhost:8202/kv/greeting
hello
$ curl -sL -X PUT -d 'written after the crash' localhost:8202/kv/note -w '%{http_code} via %{url_effective}\n' -o /dev/null
204 via http://127.0.0.1:8203/kv/note

$ scripts/cluster.sh start 1        # node 1 died holding 4 log entries
$ for i in 1 2 3; do curl -s localhost:820$i/status; done
{"id":1,"term":2,"role":"follower","leader":3,"lastIndex":7,"commitIndex":7,"lastApplied":7}
{"id":2,"term":2,"role":"follower","leader":3,"lastIndex":7,"commitIndex":7,"lastApplied":7}
{"id":3,"term":2,"role":"leader","leader":3,"lastIndex":7,"commitIndex":7,"lastApplied":7}
$ curl -sL localhost:8201/kv/note
written after the crash
```

The restarted node 1 went from 4 entries to all 7, joined term 2 as a follower, and serves the
write it missed. `lastIndex` is higher than the number of writes because each leader's no-op and
every read are log entries too.

Measured here (Apple silicon, `go test -v -run TestStress ./internal/raft`): the three stress
seeds take about 0.2 s of wall time in total with `-race` (about 0.03 s without), each simulating
20 s of chaos plus recovery on the fake clock; the three runs ended at terms 17-18 (that many
elections happened) and needed 484-1071 client attempts for 123 acknowledged operations.

## Rewrite exercises

1. Rewrite `internal/raft/node.go` from the Figure 2 comment alone, then run `go test -race
   -count=5 ./internal/raft`. Keep the test files untouched; they are the spec.
2. Break it on purpose, one rule at a time, and watch which test goes red: drop the
   `termAt(N) == n.term` check in `advanceCommit`, make `upToDate` return true, skip the
   `saveHardState` after granting a vote, truncate on every `AppendEntries`, skip the session
   lookup in `kv.Store.Apply`. Which rules does only the stress test catch, and which only a unit test?
3. Add ReadIndex: serve a read from the state machine after the leader confirms it is still
   leader with one heartbeat round, instead of appending it to the log. Write the test that
   shows a deposed leader cannot serve a stale read first.
4. Add PreVote and CheckQuorum so a cut-off leader steps down on its own and an isolated node
   rejoining does not force a fresh election. Write the failing test first (the isolated node's
   inflated term).
5. Add snapshots and log compaction (`InstallSnapshot`, a `Storage` that drops the prefix), then
   let a node that was down for a long time catch up from a snapshot.

## Deliberately left out

- **Snapshots / log compaction**: the log and WAL grow forever and a restart replays everything.
- **Membership changes**: the peer list is fixed at startup.
- **ReadIndex / lease reads**: reads go through the log (correct, but every read costs a
  replication round and a WAL write).
- **PreVote / CheckQuorum**: a leader cut off from the majority keeps believing it leads
  (its clients time out; nothing it accepts can commit), and a node returning from isolation can
  force one extra election.
- **A separate applier goroutine**: committed entries are applied inline on the loop goroutine
  and results go straight to the per-index waiter channel. Fine for an in-memory map; a slow
  state machine would stall heartbeats.
- **Group commit / pipelining**: one fsync per storage call, and a leader waits for an ack before
  advancing `nextIndex` (at most 64 entries per message).
- **Session expiry**: the dedupe table keeps one entry per client ID forever, and assumes each
  client has one request in flight.
- **Security**: the HTTP transport has no TLS or authentication.
- The linearizability checker is per key and handles at most 62 operations per key.

# lsmkv — an LSM-tree storage engine (a tiny LevelDB)

An embedded, durable, ordered key-value library: `Open(dir, opts)`, `Put`, `Get`, `Delete`,
`Scan(lo, hi)` as an `iter.Seq2`. Write-ahead log, skiplist memtable, immutable SSTables with
bloom filters, merge-iterator scans, tombstones, compaction, an atomic manifest, crash recovery
and an advisory directory lock. Plus a REPL (`cmd/lsmkv`) and a benchmark tool (`cmd/lsmbench`).

## What this project teaches

The whole design, including **every on-disk format**, is the comment at the top of `db.go`.
Retype from it.

```
Put ─► WAL append (fsync per policy) ─► memtable (skiplist) ─ full ─► flush ─► NNNNNN.sst
Get ─► memtable ─► newest SSTable ─► ... ─► oldest        (bloom filter skips tables)
Scan ─► container/heap merge of memtable + every table, newest source wins
```

- **Durability and `fsync` order** (`flush.go`, `manifest.go`, `wal.go`): a flush writes the
  table to a temp name, fsyncs, renames, fsyncs the *directory*, and only then commits by
  renaming a new manifest into place; the old WAL is deleted last. Each step survives a crash
  before the next. `SyncPolicy` (`SyncEvery` / `SyncPeriodic` / `SyncNever`) trades acknowledged
  writes against latency.
- **Torn tail vs corruption** (`decodeWAL`): a record cut short, or with a bad CRC *and nothing
  after it*, is a crash mid-append and is dropped; a bad CRC with bytes after it cannot be
  explained by a crash and is `ErrCorrupt`. Recovery truncates the file at the intact prefix so
  new appends do not land behind garbage.
- **Why append-only is fast**: `lsmbench` shows 4 µs per write with the WAL versus ~3.8 ms when
  every write waits for the disk.
- **Skiplist with one writer and lock-free readers** (`skiplist.go`): links are
  `atomic.Pointer`s and a node is fully built before the store that publishes it, bottom level
  first. Nodes are never removed, which is what makes this safe. Generic over key and value,
  with a seeded level generator so a test replays the exact shape.
- **SSTables** (`sstable.go`, `block.go`): CRC-framed data blocks, a sparse index (one entry per
  block, keyed by the block's last key), a bloom block and a footer with a magic number. Entries
  use uvarint lengths; WAL records and blocks share one entry encoding.
- **Bloom filter** (`bloom.go`): k probes derived from one 64-bit hash by double hashing. The
  hash is FNV-1a plus a finalizer, deliberately not `hash/maphash`: its seed is per process, and a
  filter persisted by one run would be garbage to the next (`TestHashKeyIsStable`).
- **Merge iterator** (`merge.go`): `container/heap` ordered by key, then by source age; on equal
  keys the newest wins and older versions are skipped. If a source fails, the merger stops rather
  than let an older source's stale value surface.
- **Tombstones and compaction** (`drain` in `flush.go`): a flush keeps tombstones (older tables
  may hold the key); compaction merges *every* table, so there is nothing older left to hide and
  it drops them. Compaction runs when 4 tables exist.
- **Readers pin a view** (`pin` in `db.go`, `table.unref`): a reader takes a reference on the
  memtable and each table, then reads without the lock. A compaction can delete files under an
  unfinished `Scan` because the last reference closes and removes them. Calling the DB from inside
  a scan loop is safe.
- **`iter.Seq2` and errors**: a `Seq2` cannot carry an error, so `Scan` returns the sequence and
  an error accessor, `sql.Rows.Err` style: `seq, errf := db.Scan(lo, hi)`, range over `seq`,
  then check `errf()`.
- **`flock`** (`lock_unix.go`, `//go:build unix`): a second `Open` of the same directory, in this
  or another process, fails with `ErrLocked`; the kernel drops the lock if the process dies.
- **Testing a storage engine**: a seeded model test, crash tests at every byte offset, a
  concurrency test and a fuzz target, below.

### The tests that matter

| Test | What it proves |
|---|---|
| `TestModel` (`model_test.go`) | 8 seeds x 500 random Put / Delete / Get / Scan / Flush / Compact / Reopen steps; after *every* step a full scan and a `Get` of every key must equal a `map[string]string`. A failure prints the seed, step, last operations and the exact `-run` line to replay it. It also asserts it triggered flushes and compactions. |
| `TestCrashTruncatedWAL` | A 50-record WAL is cut at **every** byte offset. The recovered state must equal the model after exactly the writes that were fully on disk, and the log must accept appends afterwards. |
| `TestCrashFlippedByte` | One bit flipped at every WAL offset. Outcome must be `ErrCorrupt`, or the state before the damaged record; damage inside a non-final record must be refused. |
| `TestCrashDuringFlush`, `TestCrashDuringCompaction` | The on-disk state after a crash at each step (tmp file, orphan table, committed manifest with the old WAL or inputs still present) is rebuilt from before/after snapshots; `Open` must recover everything and sweep the leftovers. |
| `TestConcurrentReadersWriterFlusher` | One writer, 4 readers, a flusher looping `Flush` + `Compact`, under `-race`. After round r completes, every reader must see every key at round >= r. |
| `FuzzDecodeBlock` | Block, checksum, handle, manifest and WAL decoders never panic on arbitrary bytes; whatever decodes re-encodes to the same entries. |
| `TestSyncPolicies` | `SyncEvery` calls `Sync` once per write (a wrapped file counts); `SyncPeriodic` is tested on a fake clock with `testing/synctest`. |

## Run

```sh
go test -race ./...
go test -race -count=5 ./...
go test -run '^$' -fuzz=FuzzDecodeBlock -fuzztime=30s .
go test -run '^$' -bench 'BenchmarkGet|BenchmarkPut' -benchmem .

# the REPL: put/get/del/scan/flush/compact/stats
D=$(mktemp -d) && go build -o $D/lsmkv ./cmd/lsmkv
printf 'put apple red fruit\nput banana yellow\nflush\nput cherry dark red\ndel apple\nscan\nstats\n' | $D/lsmkv -dir $D/demo
printf 'scan\ncompact\nstats\n' | $D/lsmkv -dir $D/demo      # reopened: the WAL is replayed
cat $D/demo/MANIFEST; ls $D/demo

# two processes, one directory: the second is refused
(sleep 5 | $D/lsmkv -dir $D/lock >/dev/null &); sleep 2
echo 'get x' | $D/lsmkv -dir $D/lock

# benchmark tool
go run ./cmd/lsmbench -n 200000 -sync never
go run ./cmd/lsmbench -n 200000 -sync never -bloom -1     # no filters
go run ./cmd/lsmbench -n 2000 -sync every
```

### Measured numbers

Apple M4, Go 1.27, a busy machine (load average 7 to 13 while measuring), so read them as
orders of magnitude. 100-byte values, no compaction.

`go test -run '^$' -bench 'BenchmarkGet|BenchmarkPut' -benchmem .` (200k keys over about 20 tables):

| Benchmark | ns/op |
|---|---|
| Get hit, bloom | 3,692 |
| Get hit, no bloom | 49,633 |
| Get miss, bloom | 1,051 |
| Get miss, no bloom | 72,918 |
| Put, `SyncEvery` (fsync per write) | 3,792,741 |
| Put, `SyncPeriodic` 10 ms | 4,207 |
| Put, `SyncNever` | 3,995 |
| Put, sequential keys | 3,476 |
| Put, random keys | 4,151 |

A miss key is an existing key plus `"x"`, so it falls inside every table's key range and only the
bloom filter can answer it without reading a block: about 70x faster. The measured
false-positive rate of the 10-bits-per-key filter is 0.1% to 1.1% (`TestBloom...`, bound 3%).
Per-write fsync is about 900x slower than not waiting; on macOS `File.Sync` is `F_FULLFSYNC`, so
this overstates what Linux with a fast NVMe would show. Sequential vs random writes differ little
here because the memtable absorbs the order: the random cost shows up as 31 overlapping tables
that every read must consult.

`lsmbench -n 200000 -sync never` (31 tables after the random-write phase):

| phase | default bloom | `-bloom -1` |
|---|---|---|
| write seq | 212k ops/s | 190k ops/s |
| write random | 199k ops/s | 176k ops/s |
| read hit | 276k ops/s | 26k ops/s |
| read miss | 732k ops/s | 10k ops/s |
| scan all | 6.4M keys/s | 6.0M keys/s |

`lsmbench -n 2000 -sync every`: 266 writes/s (3.8 ms each).

## Rewrite exercises

1. Rewrite `flushLocked`, `Open` / `recover` / `sweep` from the design comment at the top of
   `db.go` without looking at the code, then run `TestCrashDuring*` and `TestModel`. The crash tests
   tell you which step you ordered wrong.
2. Delete the old WAL *before* committing the manifest in `flushLocked`. Which test fails, and
   what data is lost? Now move the manifest commit before the table rename and repeat.
3. Add a background flush: freeze the memtable as an immutable one, let a goroutine write it
   while writes go to a fresh memtable and WAL, and make the read path consult both. Today a flush
   holds the write lock and stalls readers.
4. Replace the single full merge with leveled compaction, or with true size-tiered partial merges.
   A partial merge must keep tombstones unless it includes the oldest table; prove that with the
   model test.
5. Add an atomic `Batch` (one WAL record, one fsync) and extend `TestCrashTruncatedWAL` so recovery
   is checked at batch granularity; or add per-record header checksums to `decodeWAL` so a damaged
   length field is reported instead of being treated as a torn tail.

## Deliberately left out

- **Leveled compaction, snapshots/MVCC, block compression** (the scope guard). Compaction is the
  degenerate size-tiered case: one tier, a full merge of every table once 4 exist.
- **Background flush and compaction.** They run inline under the write lock, so a flush stalls
  readers and writers for its duration (see exercise 3).
- **Block cache, prefix compression, restart points.** Every `Get` reads and decodes a whole 4 KiB
  block.
- **Atomic multi-key batches and group commit.**
- **Windows.** The directory lock needs `syscall.Flock`, and directory fsync is assumed.
- **A damaged WAL length field pointing past EOF is indistinguishable from a torn write.** The
  result is still a prefix of the writes, but acknowledged writes after the damage are lost
  silently. Per-header checksums close this gap (exercise 5). The crash model also assumes a
  crash leaves a shorter file or a damaged final record, not a zero-filled tail.
- **A write error after the commit point poisons the DB** (`DB.failed`): every later write returns
  the error and only reopening recovers. There is no retry.

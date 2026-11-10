# searchlite — a full-text search engine

`searchlite index`, `searchlite query 'goroutine AND "memory model" -mutex'`, `searchlite serve`:
a positional inverted index with BM25 ranking, boolean and phrase queries, highlighted snippets,
a crash-safe snapshot file and live indexing over HTTP. Standard library only; a 19-document demo
corpus about Go is compiled into the binary, so everything works with zero setup.

## What this project teaches

- **The analyzer is a pipeline** — `analysis.Analyzer` is a tokenizer followed by `Filter`s;
  `Map` and `Drop` lift a plain func into a stage, and `Default()` is three of them composed.
  `Tokens` returns an `iter.Seq`, so callers that stop early do no extra work.
- **Positions survive stopwords** — a dropped word still occupies a `Pos`, which is why
  `"state of the art"` does not match "state the art" (`TestPhraseKeepsStopwordGaps`).
- **A light stemmer and its limits** — `analysis.Stem`: ~30 lines, with the failure modes
  written in the comment above it. Index and query share it, so errors cancel out.
- **The positional inverted index** — `index.Index`: term → `[]Posting{Doc, Pos}`, sorted by
  doc for free because documents are only appended. Everything else is derived from it.
- **Merge algorithms on sorted slices** — `merge` in `merge.go`: one two-cursor walk, and
  AND / OR / AND-NOT are three settings of its flags. `phrase`/`follow` fold the same walk
  over positions with `slices.BinarySearch` to find "term B exactly n words after term A".
- **BM25** — `Index.bm25`: idf from document frequency, tf saturation (k1 = 1.2), length
  normalisation (b = 0.75), all computed from the index's own statistics. A quoted phrase is
  scored as a pseudo-term: its df and tf are counted over phrase matches.
- **Top-K with a generic bounded heap** — `topk.TopK[T]` over `container/heap`: a min-heap of
  size k, so n candidates cost O(n log k) and O(k) memory.
- **A recursive-descent parser** — `query.Parse`: grammar in the comment above it, one
  function per precedence level, implicit AND, and every `ParseError` carries the byte offset
  (the HTTP API returns it, so a UI can underline the mistake).
- **"No terms" is not "no matches"** — `Index.eval` returns `ok=false` for a subtree made
  only of stopwords, and its parent ignores it: `alpha AND the` is just `alpha`.
- **Crash-safe file replacement** — `writeAtomic`: temp file in the same directory, `Sync`,
  `Rename`, fsync the directory. `TestHalfWrittenSnapshotNeverReplacesGoodOne` kills a write
  half way (an error, then a panic standing in for power loss) and checks the old snapshot is
  byte-identical and still loads.
- **Live indexing** — one `sync.RWMutex` in `Index`: `Add` takes the write lock, `Search`
  holds the read lock for the whole evaluation so it sees a consistent index.
  `TestLiveIndexing` and `TestIndexWhileSearching` hammer it under `-race`.
- **Plumbing worth reading** — `go:embed` + `fs.Sub` (`corpus`), one `fs.WalkDir` loader that
  serves both the embedded corpus and a directory (`Index.AddFS`), Go 1.22 mux patterns
  (`GET /search`, `POST /docs`), functional options (`index.WithAnalyzer`).
- **Tests** — property tests against naive references: `TestMergesMatchNaive`
  (`testing/quick`, merge vs a map-and-scan oracle) and `TestPhraseMatchesNaive` (positions vs
  substring search); a fuzz target `FuzzParse` (never panics, offsets in range, parse ∘ print
  is the identity); table tests for the stemmer, parser errors and boolean semantics.

### Query language

```
goroutine channel            implicit AND
goroutine OR channel         AND binds tighter:  a b OR c  is  (a AND b) OR c
-mutex   NOT mutex           exclusion
"memory model"               phrase: adjacent words, in this order
(a OR b) c                   grouping
```

`AND`, `OR`, `NOT` are operators only in capitals. Stemming applies everywhere: `goroutine`
matches "goroutines".

## Run

```sh
go test -race ./...
go test -run '^$' -bench . -benchmem ./internal/index
go test -fuzz=FuzzParse -fuzztime=20s ./internal/query

go run ./cmd/searchlite query 'goroutine AND "memory model" -mutex'
go run ./cmd/searchlite query -k 3 'goroutine AND "memory model"'
go run ./cmd/searchlite query '"model memory"'            # 0 matches: phrases are ordered
go run ./cmd/searchlite query 'goroutine AND )'           # error with the offset, exit 1

go run ./cmd/searchlite index -o /tmp/searchlite.idx ./internal/corpus/docs
go run ./cmd/searchlite query -i /tmp/searchlite.idx 'channel -select'

go run ./cmd/searchlite serve                             # 127.0.0.1:8101, Ctrl+C to stop
curl -s --get localhost:8101/search --data-urlencode 'q=goroutine AND "memory model" -mutex'
curl -s --get localhost:8101/search --data-urlencode 'q=goroutine AND )'
curl -s -X POST localhost:8101/docs -d '{"name":"quokka.md","text":"# Quokka\nA small marsupial."}'
curl -s --get localhost:8101/search --data-urlencode 'q=quokka'
```

Flags go before the query (`query -k 3 '...'`): that is how the `flag` package works.
Real output from the embedded corpus (`[[...]]` marks a match):

```
$ searchlite query 'goroutine AND "memory model" -mutex'
1 matches
 1.  2.173  The race detector (race-detector.md)
    ... happens when two [[goroutines]] touch the same memory, at least one of them writes, and nothing orders the accesses. The race detector instruments memory operations at build time and reports any pair ...

$ searchlite query -k 3 'goroutine AND "memory model"'
3 matches
 1.  2.859  The Go memory model (memory-model.md)
    Go [[memory]] [[model]] The [[memory]] [[model]] states when a read in one [[goroutine]] is guaranteed to observe a write made in another. Without a synchronization event between them, the read may see the old ...
 2.  2.243  Atomic operations (atomics.md)
    ... one word at a time. The [[memory]] [[model]] treats atomic operations as synchronizing, so a value stored through atomic.Int64 is visible to a [[goroutine]] that later loads it. Reach for atomics to publish a configuration pointer ...
 3.  2.173  The race detector (race-detector.md)
    ... happens when two [[goroutines]] touch the same memory, at least one of them writes, and nothing orders the accesses. The race detector instruments memory operations at build time and reports any pair ...
```

Without `-mutex` there are three matches; `memory-model.md` and `atomics.md` both mention a
mutex, so only the race detector page survives. The same queries over HTTP:

```
$ curl -s --get localhost:8101/search --data-urlencode 'q=goroutine AND )'
{"error":"query: offset 14: unexpected \")\"","offset":14}

$ curl -s -X POST localhost:8101/docs -d '{"name":"quokka.md","text":"# Quokka\nA small marsupial."}'
{"id":19,"name":"quokka.md"}
```

A second POST of the same name answers `409`; a malformed body, `400`.

### Benchmarks

`go test -run '^$' -bench . -benchmem -count=5 ./internal/index`, median of five runs, Apple
M4, **while other `go test -race` runs were hogging the CPU** (load average ~15), so read the
numbers as pessimistic and noisy. `Search` is top-10, snippets included. "Large" is a synthetic index of
20,000 documents of 100 random words each, drawn from the corpus vocabulary.

| Benchmark | corpus (19 docs) | large (20k docs) | large B/op |
|---|---|---|---|
| index the corpus (1,237 tokens) | 248 µs, 305 KB, 2,588 allocs | | |
| `goroutine` | 102 µs | 218 µs | 373 KB |
| `channel OR mutex` | 71 µs | 373 µs | 1.7 MB |
| `"memory model"` | 64 µs | 119 µs | 146 KB |
| `race detector memory` | 37 µs | 267 µs | 529 KB |
| `goroutine AND "memory model" -mutex` | 32 µs | 460 µs | 1.7 MB |

Two things the numbers (and one pprof run) show. On the tiny corpus almost nothing is search:
pprof (`-bench SearchCorpus/term -cpuprofile`) puts ~98% of `Search` in `snippet`, which
re-analyses each returned document. On the large index the negated query allocates 1.7 MB
because `Not` materialises the complement of the whole corpus — see exercise 2.

## Size

About 1,050 lines of Go source (≈850 without comments and blank lines) and ≈880 of tests,
over the 400–700 medium target. It is spread over the spec's parts, none of which could be
dropped: query parser 190, index + evaluation + BM25 + merges + phrases + snippets +
snapshot 455, analyzer + stemmer 130, HTTP + CLI 205, top-K heap 52, corpus loader 17.

## Rewrite exercises

1. Rewrite `merge` and `follow` (`merge.go`) from memory, then prove them against the property
   tests. Then make `phrase` a single k-way pass over all the lists instead of a fold.
2. Make `a AND NOT b` one `difference(eval(a), eval(b))` instead of `intersect(a, complement(b))`
   and watch the `B/op` of the last benchmark row.
3. Make `POST /docs` durable: save a snapshot after each document. Two requests can save at
   once — what orders them so the last file on disk is the newest? Add a test that fails
   without your fix.
4. Add `DELETE /docs/{name}` with tombstones (a deleted-docs bitmap consulted during
   evaluation, and BM25 statistics that exclude them), then compact on `Save`.
5. Score a title match 3× a body match: index the title as its own field and sum
   per-field BM25. Check the ranking tests still describe what you want.

## Deliberately left out

- **Update/delete and durable `POST /docs`.** Documents are immutable once added, and posted
  ones live in memory only; `searchlite index` is how a snapshot is written (exercises 3, 4).
- **Snapshot versioning and checksums.** A snapshot does not record which analyzer built it
  and a flipped bit in a string would load silently; a truncated file is detected. A crash
  during `Save` leaves a stray `*.tmp-*` file next to the snapshot (the real name is untouched);
  nothing cleans it up. Snapshots are written with mode 0600.
- **Ranking extras** — fields, phrase slop, wildcard/fuzzy terms, query-time boosts.
- **Size.** See above; the snippet re-analysis and `-NOT` complement are the two places where
  simplicity was bought with speed, on purpose.

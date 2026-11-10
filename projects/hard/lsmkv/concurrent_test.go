package lsmkv

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// The writer overwrites every key once per round with the value "<key>@<round>".
// Rounds only grow, so once round r is complete every reader must see, for
// every key, a value of its own key with round >= r: no missing keys, no
// duplicates, no values from another key, no going back in time. Meanwhile
// a flusher keeps rotating the memtable and compacting the tables, which
// is what deletes files under the readers' feet.
func TestConcurrentReadersWriterFlusher(t *testing.T) {
	const keys, rounds, readers = 60, 15, 4
	db := mustOpen(t, t.TempDir(), Options{Sync: SyncNever, MemtableBytes: 1500, BlockBytes: 128, CompactAt: 3})
	key := func(i int) string { return fmt.Sprintf("key%03d", i) }
	roundOf := func(k string, v []byte) (int, error) {
		prefix, r, ok := strings.Cut(string(v), "@")
		if !ok || prefix != k {
			return 0, fmt.Errorf("key %s holds %q", k, v)
		}
		return strconv.Atoi(r)
	}

	var completed atomic.Int64 // rounds fully written
	stop := make(chan struct{})
	errs := make(chan error, 64)
	report := func(err error) {
		select {
		case errs <- err:
		default:
		}
	}

	var wg sync.WaitGroup
	wg.Go(func() { // the one writer
		for r := 1; r <= rounds; r++ {
			for i := range keys {
				if err := db.Put(key(i), []byte(fmt.Sprintf("%s@%d", key(i), r))); err != nil {
					report(err)
					return
				}
			}
			completed.Store(int64(r))
		}
	})

	var bg sync.WaitGroup
	bg.Go(func() { // the flusher
		for {
			select {
			case <-stop:
				return
			default:
			}
			if err := errors.Join(db.Flush(), db.Compact()); err != nil {
				report(err)
				return
			}
		}
	})
	for g := range readers {
		bg.Go(func() {
			rng := rand.New(rand.NewPCG(uint64(g), 1))
			for n := 0; ; n++ {
				select {
				case <-stop:
					return
				default:
				}
				floor := int(completed.Load())
				if n%4 == 0 { // a full scan
					seen := 0
					seq, scanErr := db.Scan("", "")
					prev := ""
					for k, v := range seq {
						r, err := roundOf(k, v)
						switch {
						case err != nil:
							report(err)
							return
						case k <= prev:
							report(fmt.Errorf("scan out of order: %q after %q", k, prev))
							return
						case r < floor:
							report(fmt.Errorf("scan saw %s at round %d after round %d completed", k, r, floor))
							return
						}
						prev = k
						seen++
					}
					if err := scanErr(); err != nil {
						report(err)
						return
					}
					if floor > 0 && seen != keys {
						report(fmt.Errorf("scan saw %d keys, want %d", seen, keys))
						return
					}
					continue
				}
				k := key(rng.IntN(keys))
				v, err := db.Get(k)
				if errors.Is(err, ErrNotFound) && floor == 0 {
					continue
				}
				if err != nil {
					report(fmt.Errorf("get %s: %w", k, err))
					return
				}
				if r, err := roundOf(k, v); err != nil {
					report(err)
					return
				} else if r < floor {
					report(fmt.Errorf("get %s returned round %d after round %d completed", k, r, floor))
					return
				}
			}
		})
	}

	wg.Wait() // the writer is done (or failed)
	close(stop)
	bg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if t.Failed() {
		return
	}
	if db.next < 30 { // every flush and compaction burns file numbers
		t.Fatalf("only %d files were ever created: the flusher is not flushing", db.next)
	}
	got := dump(t, db)
	if len(got) != keys {
		t.Fatalf("final state has %d keys, want %d", len(got), keys)
	}
	for i := range keys {
		if want := fmt.Sprintf("%s@%d", key(i), rounds); got[key(i)] != want {
			t.Fatalf("final %s = %q, want %q", key(i), got[key(i)], want)
		}
	}
}

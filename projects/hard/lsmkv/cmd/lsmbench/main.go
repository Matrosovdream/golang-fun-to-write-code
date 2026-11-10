// Command lsmbench times writes and reads against a fresh lsmkv database.
//
//	lsmbench -n 200000 -sync never
//	lsmbench -n 2000 -sync every        # fsync per write is ~1000x slower: use fewer ops
//	lsmbench -n 200000 -bloom -1        # no bloom filters: compare the miss row
package main

import (
	"errors"
	"flag"
	"fmt"
	"math/rand/v2"
	"os"
	"time"

	"lsmkv"
)

func main() {
	n := flag.Int("n", 100000, "keys per phase")
	valueSize := flag.Int("value", 100, "value size in bytes")
	syncName := flag.String("sync", "never", "WAL fsync policy: every, periodic or never")
	bloom := flag.Int("bloom", 0, "bloom bits per key (0 = default 10, negative = no filter)")
	memtable := flag.Int("memtable", 1<<20, "memtable flush threshold in bytes")
	flag.Parse()

	policies := map[string]lsmkv.SyncPolicy{"every": lsmkv.SyncEvery, "periodic": lsmkv.SyncPeriodic, "never": lsmkv.SyncNever}
	policy, ok := policies[*syncName]
	if !ok {
		fmt.Fprintf(os.Stderr, "unknown -sync %q\n", *syncName)
		os.Exit(2)
	}
	opts := lsmkv.Options{Sync: policy, BloomBits: *bloom, MemtableBytes: *memtable, CompactAt: 1 << 20}

	fmt.Printf("n=%d value=%dB sync=%s bloom=%d memtable=%dB\n", *n, *valueSize, *syncName, *bloom, *memtable)
	if err := run(*n, make([]byte, *valueSize), opts); err != nil {
		fmt.Fprintln(os.Stderr, "lsmbench:", err)
		os.Exit(1)
	}
}

func key(i int) string { return fmt.Sprintf("key%010d", i) }

func run(n int, value []byte, opts lsmkv.Options) error {
	fmt.Printf("%-14s %10s %12s %10s\n", "phase", "time", "ops/s", "ns/op")
	phase := func(name string, ops int, f func() error) error {
		start := time.Now()
		if err := f(); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		d := time.Since(start)
		fmt.Printf("%-14s %10s %12.0f %10d\n", name, d.Round(time.Millisecond), float64(ops)/d.Seconds(), d.Nanoseconds()/int64(ops))
		return nil
	}
	fresh := func() (*lsmkv.DB, func(), error) {
		dir, err := os.MkdirTemp("", "lsmbench")
		if err != nil {
			return nil, nil, err
		}
		db, err := lsmkv.Open(dir, opts)
		return db, func() { db.Close(); os.RemoveAll(dir) }, err
	}
	write := func(db *lsmkv.DB, order []int) func() error {
		return func() error {
			for _, i := range order {
				if err := db.Put(key(i), value); err != nil {
					return err
				}
			}
			return nil
		}
	}
	seq := make([]int, n)
	for i := range seq {
		seq[i] = i
	}
	rng := rand.New(rand.NewPCG(1, 1))
	shuffled := rng.Perm(n)

	db, done, err := fresh()
	if err != nil {
		return err
	}
	if err := phase("write seq", n, write(db, seq)); err != nil {
		return err
	}
	done()

	db, done, err = fresh()
	if err != nil {
		return err
	}
	defer done()
	if err := phase("write random", n, write(db, shuffled)); err != nil {
		return err
	}
	if err := db.Flush(); err != nil {
		return err
	}
	fmt.Printf("  -> %d tables on disk\n", db.Stats().Tables)

	reads := min(n, 100000)
	if err := phase("read hit", reads, func() error {
		for range reads {
			if _, err := db.Get(key(rng.IntN(n))); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}
	// A miss that falls inside the key range (key + "x"), so only the filter can short-circuit it.
	if err := phase("read miss", reads, func() error {
		for range reads {
			if _, err := db.Get(key(rng.IntN(n)) + "x"); !errors.Is(err, lsmkv.ErrNotFound) {
				return fmt.Errorf("expected a miss, got %v", err)
			}
		}
		return nil
	}); err != nil {
		return err
	}
	return phase("scan all", n, func() error {
		seq, scanErr := db.Scan("", "")
		count := 0
		for range seq {
			count++
		}
		if count != n {
			return fmt.Errorf("scan saw %d keys, want %d", count, n)
		}
		return scanErr()
	})
}

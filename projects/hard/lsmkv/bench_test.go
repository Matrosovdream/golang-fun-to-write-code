package lsmkv

import (
	"fmt"
	"math/rand/v2"
	"testing"
)

// Reads: 200k keys of 100 bytes spread over ~20 tables, no compaction.
//
//	go test -run '^$' -bench 'BenchmarkGet' -benchmem
//
// A miss key is an existing key plus "x", so it sits inside every table's
// key range and only the bloom filter can answer it without reading a block.
func BenchmarkGet(b *testing.B) {
	const n = 200000
	val := make([]byte, 100)
	for _, bits := range []struct {
		name string
		bits int
	}{{"bloom", 10}, {"nobloom", -1}} {
		db := mustOpen(b, b.TempDir(), Options{Sync: SyncNever, MemtableBytes: 1 << 20, CompactAt: 1 << 20, BloomBits: bits.bits})
		for i := range n {
			if err := db.Put(fmt.Sprintf("key%08d", i), val); err != nil {
				b.Fatal(err)
			}
		}
		if err := db.Flush(); err != nil {
			b.Fatal(err)
		}
		b.Run("hit/"+bits.name, func(b *testing.B) {
			rng := rand.New(rand.NewPCG(1, 1))
			for b.Loop() {
				if _, err := db.Get(fmt.Sprintf("key%08d", rng.IntN(n))); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run("miss/"+bits.name, func(b *testing.B) {
			rng := rand.New(rand.NewPCG(1, 1))
			for b.Loop() {
				if _, err := db.Get(fmt.Sprintf("key%08d", rng.IntN(n)) + "x"); err != ErrNotFound {
					b.Fatal(err)
				}
			}
		})
		b.Logf("%s: %d tables", bits.name, db.Stats().Tables)
	}
}

// Writes: the fsync policy and the key order.
//
//	go test -run '^$' -bench 'BenchmarkPut' -benchmem
func BenchmarkPut(b *testing.B) {
	val := make([]byte, 100)
	for _, p := range []struct {
		name string
		opts Options
	}{
		{"sync=every", Options{Sync: SyncEvery}},
		{"sync=periodic10ms", Options{Sync: SyncPeriodic, SyncPeriod: 10e6}},
		{"sync=never", Options{Sync: SyncNever}},
	} {
		b.Run(p.name, func(b *testing.B) {
			db := mustOpen(b, b.TempDir(), p.opts)
			i := 0
			for b.Loop() {
				if err := db.Put(fmt.Sprintf("key%010d", i), val); err != nil {
					b.Fatal(err)
				}
				i++
			}
		})
	}
	for _, order := range []string{"sequential", "random"} {
		b.Run("order="+order, func(b *testing.B) {
			db := mustOpen(b, b.TempDir(), Options{Sync: SyncNever})
			rng := rand.New(rand.NewPCG(1, 1))
			i := 0
			for b.Loop() {
				k := i
				if order == "random" {
					k = rng.IntN(1 << 30)
				}
				if err := db.Put(fmt.Sprintf("key%010d", k), val); err != nil {
					b.Fatal(err)
				}
				i++
			}
		})
	}
}

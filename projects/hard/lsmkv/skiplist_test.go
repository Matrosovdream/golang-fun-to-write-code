package lsmkv

import (
	"fmt"
	"maps"
	"math/rand/v2"
	"slices"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

func collect(c *slCursor[string, int]) (keys []string, vals []int) {
	for k, v, ok := c.next(); ok; k, v, ok = c.next() {
		keys = append(keys, k)
		vals = append(vals, v)
	}
	return keys, vals
}

func TestSkiplistMatchesMap(t *testing.T) {
	s := newSkiplist[string, int](42)
	want := map[string]int{}
	rng := rand.New(rand.NewPCG(1, 2))
	for i := range 2000 {
		k := fmt.Sprintf("k%03d", rng.IntN(300))
		s.set(k, i) // many overwrites
		want[k] = i
	}
	for k, v := range want {
		got, ok := s.get(k)
		require.True(t, ok)
		require.Equal(t, v, got)
	}
	_, ok := s.get("absent")
	require.False(t, ok)

	sorted := slices.Sorted(maps.Keys(want))
	keys, vals := collect(s.cursor(""))
	require.Equal(t, sorted, keys)
	for i, k := range keys {
		require.Equal(t, want[k], vals[i])
	}

	keys, _ = collect(s.cursor("k150"))
	require.Equal(t, sorted[sort(sorted, "k150"):], keys)
	keys, _ = collect(s.cursor("k150x")) // between two keys: starts at the next one
	require.Equal(t, sorted[sort(sorted, "k150x"):], keys)
	keys, _ = collect(s.cursor("zzz"))
	require.Empty(t, keys)
}

func sort(sorted []string, k string) int {
	i, _ := slices.BinarySearch(sorted, k)
	return i
}

func TestSkiplistEmpty(t *testing.T) {
	s := newSkiplist[string, int](1)
	require.True(t, s.empty())
	s.set("a", 1)
	require.False(t, s.empty())
}

// The same seed must give the same tower heights, so a failing test replays.
func TestSkiplistSeededHeights(t *testing.T) {
	heights := func(seed uint64) []int {
		s := newSkiplist[string, int](seed)
		hs := make([]int, 200)
		for i := range hs {
			hs[i] = s.randomHeight()
		}
		return hs
	}
	require.Equal(t, heights(5), heights(5))
	require.NotEqual(t, heights(5), heights(6))
	count := map[int]int{}
	for _, h := range heights(5) {
		require.True(t, h >= 1 && h <= maxHeight)
		count[h]++
	}
	require.Greater(t, count[1], count[2], "level 1 should be the most common")
}

// One writer, many readers, no locks: run with -race. Keys are written in
// increasing order, so a reader that has seen key N must be able to find every key below N.
func TestSkiplistConcurrentReaders(t *testing.T) {
	s := newSkiplist[string, int](9)
	const n = 3000
	key := func(i int) string { return fmt.Sprintf("%05d", i) }
	var written atomic.Int64
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			for written.Load() < n {
				hi := int(written.Load())
				if hi > 0 {
					if _, ok := s.get(key(hi - 1)); !ok {
						t.Errorf("key %d was published but is not readable", hi-1)
						return
					}
				}
				keys, _ := collect(s.cursor(""))
				if !slices.IsSorted(keys) {
					t.Error("cursor saw unsorted keys")
					return
				}
			}
		})
	}
	for i := range n {
		s.set(key(i), i)
		s.set(key(i), i+1) // and an in-place overwrite
		written.Store(int64(i + 1))
	}
	wg.Wait()
}

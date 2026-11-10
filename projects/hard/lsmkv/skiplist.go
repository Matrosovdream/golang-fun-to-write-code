package lsmkv

import (
	"cmp"
	"math/bits"
	"math/rand/v2"
	"sync/atomic"
)

const maxHeight = 12

// skiplist is an ordered map with ONE writer and any number of concurrent
// readers, no locks: every link is an atomic pointer and a node is fully
// built before the store that makes it reachable (bottom level first), so a
// reader sees either the old list or the new one. Nodes are never removed;
// that is what makes the lock-free read safe (a memtable is dropped whole).
//
// The level generator is seeded, so a test can replay the exact tower shape.
type skiplist[K cmp.Ordered, V any] struct {
	head   *slNode[K, V]
	height atomic.Int32 // levels in use; only grows
	rng    *rand.Rand   // writer only
}

type slNode[K cmp.Ordered, V any] struct {
	key  K
	val  atomic.Pointer[V] // overwritten in place when the key is set again
	next []atomic.Pointer[slNode[K, V]]
}

func newSkiplist[K cmp.Ordered, V any](seed uint64) *skiplist[K, V] {
	s := &skiplist[K, V]{
		head: &slNode[K, V]{next: make([]atomic.Pointer[slNode[K, V]], maxHeight)},
		rng:  rand.New(rand.NewPCG(seed, seed)),
	}
	s.height.Store(1)
	return s
}

// randomHeight returns 1 with probability 3/4, 2 with 3/16, ...: the number
// of trailing zero bit-pairs of a random word, capped at maxHeight.
func (s *skiplist[K, V]) randomHeight() int {
	r := s.rng.Uint64() | 1<<(2*(maxHeight-1))
	return 1 + bits.TrailingZeros64(r)/2
}

// findGE returns the first node with key >= k. When prev is non-nil it also
// records, per level, the last node before that position (the splice points).
func (s *skiplist[K, V]) findGE(k K, prev *[maxHeight]*slNode[K, V]) *slNode[K, V] {
	x := s.head
	for lvl := int(s.height.Load()) - 1; lvl >= 0; lvl-- {
		for {
			nx := x.next[lvl].Load()
			if nx == nil || nx.key >= k {
				break
			}
			x = nx
		}
		if prev != nil {
			prev[lvl] = x
		}
	}
	return x.next[0].Load()
}

func (s *skiplist[K, V]) get(k K) (V, bool) {
	if n := s.findGE(k, nil); n != nil && n.key == k {
		return *n.val.Load(), true
	}
	var zero V
	return zero, false
}

// set must not be called concurrently with itself.
func (s *skiplist[K, V]) set(k K, v V) {
	var prev [maxHeight]*slNode[K, V]
	if n := s.findGE(k, &prev); n != nil && n.key == k {
		n.val.Store(&v)
		return
	}
	h := s.randomHeight()
	if cur := int(s.height.Load()); h > cur {
		for i := cur; i < h; i++ {
			prev[i] = s.head
		}
		s.height.Store(int32(h))
	}
	n := &slNode[K, V]{key: k, next: make([]atomic.Pointer[slNode[K, V]], h)}
	n.val.Store(&v)
	for i := range h {
		n.next[i].Store(prev[i].next[i].Load()) // point at the successor first,
		prev[i].next[i].Store(n)                // then become reachable
	}
}

func (s *skiplist[K, V]) empty() bool { return s.head.next[0].Load() == nil }

// cursor walks the bottom level from the first key >= from.
type slCursor[K cmp.Ordered, V any] struct{ n *slNode[K, V] }

func (s *skiplist[K, V]) cursor(from K) *slCursor[K, V] {
	return &slCursor[K, V]{n: s.findGE(from, nil)}
}

func (c *slCursor[K, V]) next() (K, V, bool) {
	if c.n == nil {
		var k K
		var v V
		return k, v, false
	}
	k, v := c.n.key, *c.n.val.Load()
	c.n = c.n.next[0].Load()
	return k, v, true
}

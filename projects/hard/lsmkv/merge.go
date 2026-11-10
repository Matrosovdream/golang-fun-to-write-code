package lsmkv

import "container/heap"

// source yields entries in ascending key order, one version per key.
// next reports false at the end or on error; err tells which.
type source interface {
	next() (entry, bool)
	err() error
}

// merger combines sources into one ascending stream. When several sources
// hold the same key the newest source (highest rank) wins and the older
// versions are skipped. Tombstones are passed through: the caller decides
// whether they hide the key or may be dropped.
type merger struct {
	h    mergeHeap
	fail error
}

type mergeItem struct {
	e    entry
	src  source
	rank int
}

type mergeHeap []mergeItem

func (h mergeHeap) Len() int { return len(h) }
func (h mergeHeap) Less(i, j int) bool {
	if h[i].e.key != h[j].e.key {
		return h[i].e.key < h[j].e.key
	}
	return h[i].rank > h[j].rank
}
func (h mergeHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *mergeHeap) Push(x any)   { *h = append(*h, x.(mergeItem)) }
func (h *mergeHeap) Pop() any {
	old := *h
	x := old[len(old)-1]
	*h = old[:len(old)-1]
	return x
}

// newMerger takes sources ordered oldest first.
func newMerger(srcs []source) *merger {
	m := &merger{}
	for rank, s := range srcs {
		if e, ok := s.next(); ok {
			m.h = append(m.h, mergeItem{e, s, rank})
		} else if err := s.err(); err != nil {
			m.fail = err
		}
	}
	heap.Init(&m.h)
	return m
}

func (m *merger) next() (entry, bool) {
	if m.fail != nil || len(m.h) == 0 {
		return entry{}, false
	}
	win := m.h[0].e
	for len(m.h) > 0 && m.h[0].e.key == win.key {
		top := &m.h[0]
		if e, ok := top.src.next(); ok {
			top.e = e
			heap.Fix(&m.h, 0)
		} else {
			if err := top.src.err(); err != nil {
				// An older version of win.key could now surface as the winner: stop.
				m.fail = err
				return entry{}, false
			}
			heap.Pop(&m.h)
		}
	}
	return win, true
}

func (m *merger) err() error { return m.fail }

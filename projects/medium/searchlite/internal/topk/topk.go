// Package topk keeps the best k items of a stream in O(n log k) time and O(k) space.
package topk

import "container/heap"

// TopK retains the k greatest items under less. The heap root is the smallest
// item kept so far, so a newcomer only has to beat the root to get in.
type TopK[T any] struct {
	k int
	h minHeap[T]
}

func New[T any](k int, less func(a, b T) bool) *TopK[T] {
	return &TopK[T]{k: k, h: minHeap[T]{less: less}}
}

func (t *TopK[T]) Push(x T) {
	switch {
	case t.k <= 0:
	case len(t.h.items) < t.k:
		heap.Push(&t.h, x)
	case t.h.less(t.h.items[0], x):
		t.h.items[0] = x
		heap.Fix(&t.h, 0)
	}
}

// Drain empties the TopK and returns what it held, best first: popping
// the minimum repeatedly yields worst-to-best, so fill the result from the back.
func (t *TopK[T]) Drain() []T {
	out := make([]T, len(t.h.items))
	for i := len(out) - 1; i >= 0; i-- {
		out[i] = heap.Pop(&t.h).(T)
	}
	return out
}

type minHeap[T any] struct {
	items []T
	less  func(a, b T) bool
}

func (h *minHeap[T]) Len() int           { return len(h.items) }
func (h *minHeap[T]) Less(i, j int) bool { return h.less(h.items[i], h.items[j]) }
func (h *minHeap[T]) Swap(i, j int)      { h.items[i], h.items[j] = h.items[j], h.items[i] }
func (h *minHeap[T]) Push(x any)         { h.items = append(h.items, x.(T)) }
func (h *minHeap[T]) Pop() any {
	n := len(h.items) - 1
	x := h.items[n]
	h.items = h.items[:n]
	return x
}

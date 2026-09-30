// Package cache implements a generic LRU cache with per-entry TTL.
package cache

import (
	"container/list"
	"sync"
	"time"
)

type entry[K comparable, V any] struct {
	key K
	val V
	exp time.Time
}

// LRU is safe for concurrent use. It uses a full Mutex, not RWMutex: Get
// mutates recency order (MoveToFront), so even "reads" are writes here.
type LRU[K comparable, V any] struct {
	mu    sync.Mutex
	cap   int
	ttl   time.Duration
	ll    *list.List               // front = most recently used
	items map[K]*list.Element      // key → node in ll
	now   func() time.Time         // injectable for tests
}

type Option[K comparable, V any] func(*LRU[K, V])

func WithClock[K comparable, V any](now func() time.Time) Option[K, V] {
	return func(l *LRU[K, V]) { l.now = now }
}

func New[K comparable, V any](capacity int, ttl time.Duration, opts ...Option[K, V]) *LRU[K, V] {
	l := &LRU[K, V]{
		cap:   capacity,
		ttl:   ttl,
		ll:    list.New(),
		items: make(map[K]*list.Element, capacity),
		now:   time.Now,
	}
	for _, opt := range opts {
		opt(l)
	}
	return l
}

func (l *LRU[K, V]) Get(key K) (V, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	el, ok := l.items[key]
	if !ok {
		var zero V
		return zero, false
	}
	ent := el.Value.(*entry[K, V])
	if l.now().After(ent.exp) {
		// Lazy expiry: entries die when observed, no janitor goroutine needed.
		l.ll.Remove(el)
		delete(l.items, key)
		var zero V
		return zero, false
	}
	l.ll.MoveToFront(el)
	return ent.val, true
}

func (l *LRU[K, V]) Set(key K, val V) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if el, ok := l.items[key]; ok {
		ent := el.Value.(*entry[K, V])
		ent.val = val
		ent.exp = l.now().Add(l.ttl)
		l.ll.MoveToFront(el)
		return
	}

	l.items[key] = l.ll.PushFront(&entry[K, V]{key: key, val: val, exp: l.now().Add(l.ttl)})
	if l.ll.Len() > l.cap {
		oldest := l.ll.Back()
		l.ll.Remove(oldest)
		delete(l.items, oldest.Value.(*entry[K, V]).key)
	}
}

func (l *LRU[K, V]) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.ll.Len()
}

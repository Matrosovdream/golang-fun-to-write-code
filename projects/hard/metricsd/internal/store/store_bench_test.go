package store_test

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"metricsd/internal/parse"
	"metricsd/internal/store"
)

// Three designs for the same job, benchmarked head-to-head:
//
//	go test -bench=BenchmarkStores -benchmem ./internal/store
//
// Measured on M1 Pro (10 cores): globalMutex ~128ns/op (every core queues
// on one lock), sharded ~55ns/op, and sync.Map ~9ns/op — the WINNER here,
// which surprises most people. On a stable key set sync.Map reads are a
// lock-free load of an immutable "read" map: cheaper than even an RLock.
// This is exactly the workload sync.Map's doc says it is for.
//
// So why does the real store shard RWMutex maps instead? Because it needs
// what this microbenchmark doesn't measure: three typed maps updated
// together, consistent snapshot iteration, and stable performance when new
// keys keep arriving (key churn forces sync.Map through its slow dirty-map
// path). Benchmark before believing — including me: rerun this with key
// churn (append a growing suffix to names) and watch the ranking flip.

// -- rival 1: one big mutex around plain maps --
type globalMutexStore struct {
	mu       sync.Mutex
	counters map[string]int64
}

func (g *globalMutexStore) Apply(ev parse.Event) {
	g.mu.Lock()
	g.counters[string(ev.Name)] += ev.Value
	g.mu.Unlock()
}

// -- rival 2: sync.Map of atomics --
type syncMapStore struct {
	counters sync.Map // string → *atomic.Int64
}

func (s *syncMapStore) Apply(ev parse.Event) {
	v, ok := s.counters.Load(string(ev.Name))
	if !ok {
		v, _ = s.counters.LoadOrStore(string(ev.Name), new(atomic.Int64))
	}
	v.(*atomic.Int64).Add(ev.Value)
}

type applier interface{ Apply(parse.Event) }

func benchStore(b *testing.B, s applier) {
	// 256 distinct metric names, pre-rendered so the benchmark measures the
	// store, not fmt.
	names := make([][]byte, 256)
	for i := range names {
		names[i] = fmt.Appendf(nil, "metric_%d", i)
	}
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			s.Apply(parse.Event{Kind: parse.KindCounter, Name: names[i&255], Value: 1})
			i++
		}
	})
}

func BenchmarkStoresSharded(b *testing.B) {
	benchStore(b, store.New())
}

func BenchmarkStoresGlobalMutex(b *testing.B) {
	benchStore(b, &globalMutexStore{counters: make(map[string]int64)})
}

func BenchmarkStoresSyncMap(b *testing.B) {
	benchStore(b, &syncMapStore{})
}

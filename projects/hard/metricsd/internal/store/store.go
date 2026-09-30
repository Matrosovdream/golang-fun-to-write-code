// Package store is the in-memory metrics engine, built for very high
// concurrent write rates.
//
// Design, from the outside in:
//   - 64 shards, picked by hash of the metric name → contention divides by 64;
//   - inside a shard, a read-mostly RWMutex protects only the MAPS, never the
//     values: the hot path takes the shared lock, finds an *atomic.Int64 and
//     updates it lock-free. The exclusive lock is only for first-seen names;
//   - histograms are fixed log-scale buckets of atomics — recording is
//     lock-free too, and percentiles are estimated at read time.
//
// The benchmark file pits this against a single-mutex store and sync.Map.
package store

import (
	"hash/maphash"
	"sync"
	"sync/atomic"

	"metricsd/internal/parse"
)

const shardCount = 64 // power of two so the mask trick works

type Store struct {
	shards [shardCount]shard
	seed   maphash.Seed
	ops    atomic.Int64
}

type shard struct {
	mu       sync.RWMutex
	counters map[string]*atomic.Int64
	gauges   map[string]*atomic.Int64
	hists    map[string]*histogram
}

func New() *Store {
	s := &Store{seed: maphash.MakeSeed()}
	for i := range s.shards {
		s.shards[i].counters = make(map[string]*atomic.Int64)
		s.shards[i].gauges = make(map[string]*atomic.Int64)
		s.shards[i].hists = make(map[string]*histogram)
	}
	return s
}

// Apply routes one parsed event. ev.Name is a borrowed []byte: it is only
// converted to a string when a new metric is created (the one place that
// must allocate). Map lookups with string(b) compile to zero-copy probes.
func (s *Store) Apply(ev parse.Event) {
	s.ops.Add(1)
	sh := &s.shards[maphash.Bytes(s.seed, ev.Name)&(shardCount-1)]

	switch ev.Kind {
	case parse.KindCounter:
		counter(sh, ev.Name).Add(ev.Value)
	case parse.KindGauge:
		gauge(sh, ev.Name).Store(ev.Value)
	case parse.KindHistogram:
		hist(sh, ev.Name).observe(ev.Value)
	}
}

func (s *Store) Ops() int64 { return s.ops.Load() }

// counter implements the read-mostly locking dance; gauge and hist repeat
// it. (A generics version is possible — try it as an exercise — but the
// three-copy version keeps the pattern readable.)
func counter(sh *shard, name []byte) *atomic.Int64 {
	sh.mu.RLock()
	v, ok := sh.counters[string(name)] // no allocation: lookup-only conversion
	sh.mu.RUnlock()
	if ok {
		return v
	}

	sh.mu.Lock()
	defer sh.mu.Unlock()
	// Double-check: another goroutine may have created it between locks.
	if v, ok = sh.counters[string(name)]; ok {
		return v
	}
	v = new(atomic.Int64)
	sh.counters[string(name)] = v // allocates the key string — once per metric
	return v
}

func gauge(sh *shard, name []byte) *atomic.Int64 {
	sh.mu.RLock()
	v, ok := sh.gauges[string(name)]
	sh.mu.RUnlock()
	if ok {
		return v
	}
	sh.mu.Lock()
	defer sh.mu.Unlock()
	if v, ok = sh.gauges[string(name)]; ok {
		return v
	}
	v = new(atomic.Int64)
	sh.gauges[string(name)] = v
	return v
}

func hist(sh *shard, name []byte) *histogram {
	sh.mu.RLock()
	h, ok := sh.hists[string(name)]
	sh.mu.RUnlock()
	if ok {
		return h
	}
	sh.mu.Lock()
	defer sh.mu.Unlock()
	if h, ok = sh.hists[string(name)]; ok {
		return h
	}
	h = &histogram{}
	sh.hists[string(name)] = h
	return h
}

// --- snapshots (read path, for the HTTP API) ---

type HistSnapshot struct {
	Count int64   `json:"count"`
	Sum   int64   `json:"sum"`
	P50   float64 `json:"p50"`
	P95   float64 `json:"p95"`
	P99   float64 `json:"p99"`
}

type Snapshot struct {
	Counters   map[string]int64        `json:"counters"`
	Gauges     map[string]int64        `json:"gauges"`
	Histograms map[string]HistSnapshot `json:"histograms"`
}

func (s *Store) Snapshot() Snapshot {
	snap := Snapshot{
		Counters:   make(map[string]int64),
		Gauges:     make(map[string]int64),
		Histograms: make(map[string]HistSnapshot),
	}
	for i := range s.shards {
		sh := &s.shards[i]
		sh.mu.RLock()
		for name, v := range sh.counters {
			snap.Counters[name] = v.Load()
		}
		for name, v := range sh.gauges {
			snap.Gauges[name] = v.Load()
		}
		for name, h := range sh.hists {
			snap.Histograms[name] = h.snapshot()
		}
		sh.mu.RUnlock()
	}
	return snap
}

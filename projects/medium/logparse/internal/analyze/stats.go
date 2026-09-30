package analyze

import (
	"slices"
	"sort"

	"logparse/internal/parse"
)

type Stats struct {
	Total     int64
	Malformed int64
	ByStatus  map[int]int64
	ByIP      map[string]int64
	ByPath    map[string]int64
	latencies []float64
}

func NewStats() *Stats {
	return &Stats{
		ByStatus: make(map[int]int64),
		ByIP:     make(map[string]int64),
		ByPath:   make(map[string]int64),
		// Preallocating avoids repeated grow-and-copy while the slice fills —
		// one of the cheapest optimizations in Go.
		latencies: make([]float64, 0, 4096),
	}
}

func (s *Stats) Record(l parse.Line) {
	s.Total++
	s.ByStatus[l.Status]++
	s.ByIP[l.IP]++
	s.ByPath[l.Path]++
	s.latencies = append(s.latencies, l.LatencyMs)
}

// Merge folds other into s — this is what makes parallel analysis possible:
// every worker fills its own Stats with zero locking, and merging happens
// once at the end.
func (s *Stats) Merge(other *Stats) {
	s.Total += other.Total
	s.Malformed += other.Malformed
	for k, v := range other.ByStatus {
		s.ByStatus[k] += v
	}
	for k, v := range other.ByIP {
		s.ByIP[k] += v
	}
	for k, v := range other.ByPath {
		s.ByPath[k] += v
	}
	s.latencies = append(s.latencies, other.latencies...)
}

// Percentile sorts lazily on first use; p is 0..100.
func (s *Stats) Percentile(p float64) float64 {
	if len(s.latencies) == 0 {
		return 0
	}
	if !sort.Float64sAreSorted(s.latencies) {
		slices.Sort(s.latencies)
	}
	idx := int(p / 100 * float64(len(s.latencies)-1))
	return s.latencies[idx]
}

type Pair[K comparable] struct {
	Key   K
	Count int64
}

// TopN works for any counter map thanks to generics — map[string]int64 and
// map[int]int64 both go through the same code.
func TopN[K comparable](m map[K]int64, n int) []Pair[K] {
	pairs := make([]Pair[K], 0, len(m))
	for k, v := range m {
		pairs = append(pairs, Pair[K]{k, v})
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].Count > pairs[j].Count })
	if len(pairs) > n {
		pairs = pairs[:n]
	}
	return pairs
}

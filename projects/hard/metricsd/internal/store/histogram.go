package store

import "sync/atomic"

// bounds are log-scale upper bucket edges (the last bucket is +inf).
// Fixed buckets trade exactness for O(1) memory per metric — the same deal
// Prometheus histograms make.
var bounds = [...]int64{1, 2, 5, 10, 25, 50, 100, 250, 500, 1000, 2500, 5000, 10000}

type histogram struct {
	buckets [len(bounds) + 1]atomic.Int64
	count   atomic.Int64
	sum     atomic.Int64
}

// observe is lock-free: two atomic adds and one atomic increment.
func (h *histogram) observe(v int64) {
	h.buckets[bucketFor(v)].Add(1)
	h.count.Add(1)
	h.sum.Add(v)
}

func bucketFor(v int64) int {
	for i, b := range bounds {
		if v <= b {
			return i
		}
	}
	return len(bounds)
}

func (h *histogram) snapshot() HistSnapshot {
	return HistSnapshot{
		Count: h.count.Load(),
		Sum:   h.sum.Load(),
		P50:   h.percentile(50),
		P95:   h.percentile(95),
		P99:   h.percentile(99),
	}
}

// percentile walks the buckets until the target rank is inside one, then
// answers with that bucket's upper bound — an estimate whose error is the
// bucket width, which is exactly the precision we paid for.
func (h *histogram) percentile(p float64) float64 {
	total := h.count.Load()
	if total == 0 {
		return 0
	}
	rank := int64(p / 100 * float64(total))
	var seen int64
	for i := range h.buckets {
		seen += h.buckets[i].Load()
		if seen > rank {
			if i < len(bounds) {
				return float64(bounds[i])
			}
			return float64(bounds[len(bounds)-1]) * 2 // +inf bucket: best guess
		}
	}
	return 0
}

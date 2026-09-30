package store_test

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"metricsd/internal/parse"
	"metricsd/internal/store"
)

func ev(kind parse.Kind, name string, v int64) parse.Event {
	return parse.Event{Kind: kind, Name: []byte(name), Value: v}
}

func TestCountersGaugesHistograms(t *testing.T) {
	s := store.New()

	s.Apply(ev(parse.KindCounter, "reqs", 1))
	s.Apply(ev(parse.KindCounter, "reqs", 4))
	s.Apply(ev(parse.KindGauge, "depth", 10))
	s.Apply(ev(parse.KindGauge, "depth", 7)) // gauges overwrite
	for i := int64(1); i <= 100; i++ {
		s.Apply(ev(parse.KindHistogram, "lat", i*10))
	}

	snap := s.Snapshot()
	require.EqualValues(t, 5, snap.Counters["reqs"])
	require.EqualValues(t, 7, snap.Gauges["depth"])

	h := snap.Histograms["lat"]
	require.EqualValues(t, 100, h.Count)
	require.EqualValues(t, 50500, h.Sum)
	// Percentiles are bucket-precision estimates: the answer is the upper
	// edge of the containing bucket, so only bucket-level bounds hold.
	require.GreaterOrEqual(t, h.P50, 500.0)
	require.LessOrEqual(t, h.P50, 1000.0)
	require.LessOrEqual(t, h.P50, h.P95)
	require.LessOrEqual(t, h.P95, h.P99)
	require.EqualValues(t, 104, s.Ops())
}

// Correctness under contention: many goroutines hammering the same and
// different counters; the final sums must be exact. Run with -race.
func TestConcurrentApplyIsExact(t *testing.T) {
	s := store.New()
	const goroutines, perG = 16, 1000

	var wg sync.WaitGroup
	for g := range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			own := fmt.Sprintf("own_%d", g)
			for range perG {
				s.Apply(ev(parse.KindCounter, "shared", 1))
				s.Apply(ev(parse.KindCounter, own, 1))
			}
		}()
	}
	wg.Wait()

	snap := s.Snapshot()
	require.EqualValues(t, goroutines*perG, snap.Counters["shared"])
	for g := range goroutines {
		require.EqualValues(t, perG, snap.Counters[fmt.Sprintf("own_%d", g)])
	}
}

package crawler

import (
	"context"
	"sync/atomic"

	"golang.org/x/sync/errgroup"
)

type CheckStats struct {
	OK     atomic.Int64
	Broken atomic.Int64
	Failed atomic.Int64
}

// CheckAll probes a flat list of URLs concurrently. Compare with Crawl: no
// coordinator is needed because the work list is known up front, so
// errgroup with SetLimit is the whole pattern.
func (c *Crawler) CheckAll(ctx context.Context, urls []string) ([]Result, *CheckStats, error) {
	stats := &CheckStats{}
	results := make([]Result, len(urls))

	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(c.workers) // bounded concurrency, like a semaphore

	for i, u := range urls {
		g.Go(func() error {
			res := c.fetch(ctx, u, false)
			// Each goroutine writes only its own index, so the slice needs
			// no lock; the counters are shared and must be atomic.
			results[i] = res
			switch {
			case res.Err != nil:
				stats.Failed.Add(1)
			case res.Status >= 400:
				stats.Broken.Add(1)
			default:
				stats.OK.Add(1)
			}
			return ctx.Err() // stop scheduling more work once cancelled
		})
	}

	err := g.Wait()
	return results, stats, err
}

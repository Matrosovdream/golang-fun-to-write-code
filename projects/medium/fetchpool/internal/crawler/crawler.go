package crawler

import (
	"context"
	"net/http"
	"net/url"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type Crawler struct {
	client   *http.Client
	limiter  *rate.Limiter
	workers  int
	maxDepth int
	sameHost bool
}

type Option func(*Crawler)

func WithWorkers(n int) Option        { return func(c *Crawler) { c.workers = n } }
func WithMaxDepth(d int) Option       { return func(c *Crawler) { c.maxDepth = d } }
func WithRate(rps float64) Option     { return func(c *Crawler) { c.limiter = rate.NewLimiter(rate.Limit(rps), 1) } }
func WithAnyHost() Option             { return func(c *Crawler) { c.sameHost = false } }
func WithClient(h *http.Client) Option { return func(c *Crawler) { c.client = h } }

func New(opts ...Option) *Crawler {
	c := &Crawler{
		client:   &http.Client{Timeout: 10 * time.Second},
		limiter:  rate.NewLimiter(20, 1),
		workers:  8,
		maxDepth: 2,
		sameHost: true,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

type job struct {
	url   string
	depth int
}

// Crawl walks the link graph from start, breadth-ish, with a fixed worker
// pool. The frontier (visited set + queue) is owned by a single coordinator
// loop, so it needs no mutex: state is confined to one goroutine and
// everything else communicates over channels.
func (c *Crawler) Crawl(ctx context.Context, start string) ([]Result, error) {
	startURL, err := url.Parse(start)
	if err != nil {
		return nil, err
	}

	jobs := make(chan job)
	results := make(chan Result)

	var wg sync.WaitGroup
	for range c.workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				// Only pages above maxDepth need their links parsed.
				res := c.fetch(ctx, j.url, j.depth < c.maxDepth)
				res.depth = j.depth
				select {
				case results <- res:
				case <-ctx.Done():
					return
				}
			}
		}()
	}

	// The coordinator: pending counts jobs queued or in flight. When it
	// drops to zero the crawl is complete and jobs can be closed.
	var (
		all     []Result
		queue   = []job{{start, 0}}
		visited = map[string]bool{start: true}
		pending = 1
	)
loop:
	for pending > 0 {
		// A nil channel blocks forever in select — the standard trick to
		// disable the "send a job" case while the queue is empty.
		var out chan<- job
		var next job
		if len(queue) > 0 {
			out = jobs
			next = queue[0]
		}

		select {
		case out <- next:
			queue = queue[1:]
		case res := <-results:
			pending--
			all = append(all, res)
			for _, l := range res.Links {
				if visited[l] || !c.allowed(startURL, l) {
					continue
				}
				visited[l] = true
				queue = append(queue, job{l, res.depth + 1})
				pending++
			}
		case <-ctx.Done():
			break loop
		}
	}

	close(jobs)
	wg.Wait()
	return all, ctx.Err()
}

func (c *Crawler) allowed(start *url.URL, raw string) bool {
	if !c.sameHost {
		return true
	}
	u, err := url.Parse(raw)
	return err == nil && u.Host == start.Host
}

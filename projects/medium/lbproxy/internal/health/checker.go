// Package health runs active health checks: periodically GET a path on every
// backend and feed the verdict into the backend's hysteresis state machine.
package health

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"lbproxy/internal/lb"
)

type Checker struct {
	Backends []*lb.Backend
	Path     string
	Timeout  time.Duration
	Client   *http.Client
	Log      *slog.Logger
}

// Run does one round per tick until ctx is done. The ticker is injected so
// tests drive time by hand instead of sleeping.
func (c *Checker) Run(ctx context.Context, tick <-chan time.Time) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick:
			c.Round(ctx)
		}
	}
}

// Round probes every backend concurrently and returns when all have answered.
func (c *Checker) Round(ctx context.Context) {
	var wg sync.WaitGroup
	for _, b := range c.Backends {
		wg.Go(func() {
			ok := c.probe(ctx, b)
			if ctx.Err() != nil {
				return // shutting down or replaced by a reload: not the backend's fault
			}
			if b.Observe(ok) {
				c.Log.Warn("backend health changed", "backend", b.URL.Host, "healthy", b.Healthy())
			}
		})
	}
	wg.Wait()
}

func (c *Checker) probe(ctx context.Context, b *lb.Backend) bool {
	ctx, cancel := context.WithTimeout(ctx, c.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.URL.JoinPath(c.Path).String(), nil)
	if err != nil {
		return false
	}
	resp, err := c.Client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10)) // drain so the connection can be reused
	return resp.StatusCode/100 == 2
}

// Package httpx holds reusable HTTP client plumbing.
package httpx

import (
	"fmt"
	"math/rand/v2"
	"net/http"
	"time"
)

// RetryTransport is an http.RoundTripper decorator: it wraps another
// transport and adds retries with exponential backoff + jitter. Plugging in
// at the transport level means every request through the client gets the
// behavior — no call sites change.
//
// The same idea at library scale: github.com/cenkalti/backoff.
type RetryTransport struct {
	Next        http.RoundTripper
	MaxAttempts int
	BaseDelay   time.Duration
}

func (t *RetryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	next := t.Next
	if next == nil {
		next = http.DefaultTransport
	}

	// Only idempotent, body-less requests can be retried blindly; a POST
	// body would already be consumed on the second attempt.
	if req.Method != http.MethodGet && req.Method != http.MethodHead {
		return next.RoundTrip(req)
	}

	var lastErr error
	for attempt := 0; attempt < t.MaxAttempts; attempt++ {
		if attempt > 0 {
			select {
			case <-time.After(t.backoff(attempt)):
			case <-req.Context().Done():
				return nil, req.Context().Err()
			}
		}

		resp, err := next.RoundTrip(req)
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode >= 500 {
			resp.Body.Close() // must drain/close before retrying, or the connection leaks
			lastErr = fmt.Errorf("server returned %s", resp.Status)
			continue
		}
		return resp, nil
	}
	return nil, fmt.Errorf("giving up after %d attempts: %w", t.MaxAttempts, lastErr)
}

// backoff grows base·2^(attempt-1) with ±25% jitter. Jitter prevents a
// thundering herd of clients retrying in lockstep.
func (t *RetryTransport) backoff(attempt int) time.Duration {
	d := t.BaseDelay << (attempt - 1)
	jitter := time.Duration(rand.Int64N(int64(d) / 2))
	return d*3/4 + jitter
}

// Package lb holds the load-balancing core: backends with health state,
// the balancing strategies, and the pool that ties them together.
package lb

import (
	"net/url"
	"sync"
	"sync/atomic"
	"time"
)

// Policy is the per-pool health tuning every backend in the pool shares.
type Policy struct {
	Rise, Fall int           // consecutive probes needed to go up / down
	MaxFails   int           // consecutive request failures that eject
	Cooldown   time.Duration // how long an ejected backend sits out
}

// Backend carries two independent health signals, and is Available only when
// both are happy:
//
//   - active: probe results pass through a hysteresis state machine, so one
//     lost probe does not flap the backend (Observe);
//   - passive: real request failures eject it for a cooldown (Report).
type Backend struct {
	URL    *url.URL
	Weight int
	pol    Policy

	inflight, requests, failures atomic.Int64

	mu     sync.Mutex
	up     bool      // active-check verdict
	streak int       // consecutive probes that disagree with up
	fails  int       // consecutive request failures
	until  time.Time // ejected until
}

func NewBackend(u *url.URL, weight int, pol Policy) *Backend {
	return &Backend{URL: u, Weight: weight, pol: pol, up: true}
}

func (b *Backend) Acquire()        { b.inflight.Add(1); b.requests.Add(1) }
func (b *Backend) Release()        { b.inflight.Add(-1) }
func (b *Backend) Inflight() int64 { return b.inflight.Load() }

// Observe feeds one probe result into the state machine and reports whether
// the backend changed state. A probe that agrees with the current state resets
// the streak; Fall (or Rise) disagreeing probes in a row flip it.
func (b *Backend) Observe(ok bool) (changed bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if ok == b.up {
		b.streak = 0
		return false
	}
	b.streak++
	need := b.pol.Fall
	if ok {
		need = b.pol.Rise
	}
	if b.streak < need {
		return false
	}
	b.up, b.streak = ok, 0
	return true
}

// Report records the outcome of one proxied request. fails is deliberately
// not reset on ejection: once the cooldown ends the backend is half-open, and
// a single further failure ejects it again, while a success closes it.
func (b *Backend) Report(ok bool, now time.Time) (ejected bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if ok {
		b.fails = 0
		return false
	}
	b.failures.Add(1)
	b.fails++
	if b.fails < b.pol.MaxFails {
		return false
	}
	ejected = !now.Before(b.until)
	b.until = now.Add(b.pol.Cooldown)
	return ejected
}

func (b *Backend) Available(now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.up && !now.Before(b.until)
}

func (b *Backend) Healthy() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.up
}

// Stats is the JSON shape of one backend in /lb/status.
type Stats struct {
	URL      string `json:"url"`
	Weight   int    `json:"weight"`
	Healthy  bool   `json:"healthy"`
	Ejected  bool   `json:"ejected"`
	Inflight int64  `json:"inflight"`
	Requests int64  `json:"requests"`
	Failures int64  `json:"failures"`
}

func (b *Backend) Stats(now time.Time) Stats {
	b.mu.Lock()
	up, ejected := b.up, now.Before(b.until)
	b.mu.Unlock()
	return Stats{b.URL.String(), b.Weight, up, ejected, b.Inflight(), b.requests.Load(), b.failures.Load()}
}

package clock

import (
	"slices"
	"sync"
	"time"
)

// Fake only moves when Advance is called. Timers due inside one Advance fire
// in deadline order; callbacks run on the advancing goroutine, so they must
// not block.
type Fake struct {
	mu     sync.Mutex
	now    time.Time
	timers []*timer
}

type timer struct {
	at     time.Time
	period time.Duration // 0 for a one-shot
	fire   func(time.Time)
}

func NewFake() *Fake { return &Fake{now: time.Unix(0, 0)} }

func (f *Fake) AfterFunc(d time.Duration, fn func()) {
	f.add(d, 0, func(time.Time) { fn() })
}

func (f *Fake) NewTicker(d time.Duration) Ticker {
	ch := make(chan time.Time, 1)
	t := f.add(d, d, func(now time.Time) {
		select { // like time.Ticker: a slow receiver misses ticks, it never blocks the clock
		case ch <- now:
		default:
		}
	})
	return &fakeTicker{f: f, t: t, ch: ch}
}

func (f *Fake) add(d, period time.Duration, fire func(time.Time)) *timer {
	f.mu.Lock()
	defer f.mu.Unlock()
	t := &timer{at: f.now.Add(d), period: period, fire: fire}
	f.timers = append(f.timers, t)
	return t
}

func (f *Fake) Advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	target := f.now.Add(d)
	for {
		i := f.earliest(target)
		if i < 0 {
			break
		}
		t := f.timers[i]
		at := t.at
		if t.period > 0 {
			t.at = at.Add(t.period)
		} else {
			f.timers = slices.Delete(f.timers, i, i+1)
		}
		f.now = at
		f.mu.Unlock()
		t.fire(at)
		f.mu.Lock()
	}
	f.now = target
}

// earliest returns the index of the timer due first at or before target, or -1.
func (f *Fake) earliest(target time.Time) int {
	best := -1
	for i, t := range f.timers {
		if !t.at.After(target) && (best < 0 || t.at.Before(f.timers[best].at)) {
			best = i
		}
	}
	return best
}

type fakeTicker struct {
	f  *Fake
	t  *timer
	ch chan time.Time
}

func (t *fakeTicker) C() <-chan time.Time { return t.ch }

func (t *fakeTicker) Stop() {
	t.f.mu.Lock()
	defer t.f.mu.Unlock()
	t.f.timers = slices.DeleteFunc(t.f.timers, func(x *timer) bool { return x == t.t })
}

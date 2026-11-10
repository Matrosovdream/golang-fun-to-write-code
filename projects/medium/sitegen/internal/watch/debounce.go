package watch

import (
	"context"
	"time"
)

// Clock is the one thing Debounce needs from time, so tests can fire timers by hand.
type Clock interface {
	After(d time.Duration) <-chan time.Time
}

type RealClock struct{}

func (RealClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

// Debounce collapses a burst of events on in into one signal on the returned
// channel, sent once wait has passed since the last event. Every event
// replaces the pending timer, so a storm of five saves-worth of events
// produces a single signal.
//
// The output has room for one signal: if the consumer is still busy with the
// previous one, a second pending signal is enough to say "something changed".
// It is closed when ctx is done or in is closed.
func Debounce[T any](ctx context.Context, in <-chan T, wait time.Duration, clock Clock) <-chan struct{} {
	out := make(chan struct{}, 1)
	go func() {
		defer close(out)
		var fire <-chan time.Time // nil (blocks forever) while nothing is pending
		for {
			select {
			case <-ctx.Done():
				return
			case _, ok := <-in:
				if !ok {
					return
				}
				fire = clock.After(wait)
			case <-fire:
				fire = nil
				select {
				case out <- struct{}{}:
				default:
				}
			}
		}
	}()
	return out
}

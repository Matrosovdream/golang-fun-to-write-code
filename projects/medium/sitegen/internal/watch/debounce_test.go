package watch

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// fakeClock hands each After call's timer to the test, which fires it by hand.
type fakeClock struct{ timers chan chan time.Time }

func newFakeClock() *fakeClock { return &fakeClock{timers: make(chan chan time.Time, 16)} }

func (c *fakeClock) After(time.Duration) <-chan time.Time {
	ch := make(chan time.Time, 1)
	c.timers <- ch
	return ch
}

// send delivers an event and waits for the debouncer to start its timer. in is
// unbuffered, so once the timer exists the debouncer is back at its select.
func send(in chan<- string, clock *fakeClock, name string) chan time.Time {
	in <- name
	return <-clock.timers
}

func quiet(t *testing.T, out <-chan struct{}) {
	t.Helper()
	select {
	case <-out:
		t.Fatal("unexpected signal")
	default:
	}
}

func TestDebounceCollapsesAStorm(t *testing.T) {
	clock := newFakeClock()
	in := make(chan string)
	out := Debounce(t.Context(), in, time.Second, clock)

	var timers []chan time.Time
	for _, name := range []string{"a.md~", "a.md", "a.md", "4913", "a.md"} {
		timers = append(timers, send(in, clock, name))
		quiet(t, out) // nothing fires until a timer does
	}
	timers[len(timers)-1] <- time.Now()
	<-out

	send(in, clock, "b.md") // sync point: the debouncer has finished handling the signal
	quiet(t, out)
}

func TestDebounceIgnoresSupersededTimers(t *testing.T) {
	clock := newFakeClock()
	in := make(chan string)
	out := Debounce(t.Context(), in, time.Second, clock)

	first := send(in, clock, "a")
	second := send(in, clock, "b")
	live := send(in, clock, "c")
	first <- time.Now() // each of these expired, but a later event restarted the wait
	second <- time.Now()
	send(in, clock, "d")
	quiet(t, out)
	live <- time.Now()
	quiet(t, out)
}

func TestDebounceKeepsOnePendingSignalForABusyConsumer(t *testing.T) {
	clock := newFakeClock()
	in := make(chan string)
	out := Debounce(t.Context(), in, time.Second, clock)

	send(in, clock, "a") <- time.Now() // consumer is busy and does not read out
	send(in, clock, "b") <- time.Now()
	send(in, clock, "c") // sync point: both signals have been handled
	<-out
	quiet(t, out)
}

func TestDebounceStops(t *testing.T) {
	t.Run("input closed", func(t *testing.T) {
		in := make(chan string)
		out := Debounce(t.Context(), in, time.Second, newFakeClock())
		close(in)
		_, ok := <-out
		require.False(t, ok)
	})

	t.Run("context cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		out := Debounce(ctx, make(chan string), time.Second, newFakeClock())
		cancel()
		_, ok := <-out
		require.False(t, ok)
	})
}

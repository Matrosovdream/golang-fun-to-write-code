package orders

import (
	"fmt"
	"sync"
	"time"
)

type BreakerOpenError struct {
	Until time.Time
}

func (e *BreakerOpenError) Error() string {
	return fmt.Sprintf("circuit open until %s", e.Until.Format(time.RFC3339))
}

// Breaker is a minimal circuit breaker: after maxFails consecutive failures
// it "opens" and fails fast for cooldown, then lets one probe through
// ("half-open"). Production version: sony/gobreaker.
type Breaker struct {
	mu       sync.Mutex
	fails    int
	openTill time.Time

	maxFails int
	cooldown time.Duration
}

func NewBreaker(maxFails int, cooldown time.Duration) *Breaker {
	return &Breaker{maxFails: maxFails, cooldown: cooldown}
}

func (b *Breaker) Do(fn func() error) error {
	b.mu.Lock()
	if time.Now().Before(b.openTill) {
		defer b.mu.Unlock()
		// Failing fast is the point: no goroutine piles up waiting on a
		// dependency that is already known to be down.
		return &BreakerOpenError{Until: b.openTill}
	}
	b.mu.Unlock()

	err := fn()

	b.mu.Lock()
	defer b.mu.Unlock()
	if err == nil {
		b.fails = 0
		return nil
	}
	b.fails++
	if b.fails >= b.maxFails {
		b.openTill = time.Now().Add(b.cooldown)
		b.fails = 0
	}
	return err
}

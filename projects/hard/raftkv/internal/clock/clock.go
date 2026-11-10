// Package clock is the time seam: the node binary runs on Real, tests run on
// Fake and advance it by hand, so a minute of cluster time costs microseconds.
package clock

import "time"

// Clock is everything the node and the simulated network need from time.
type Clock interface {
	NewTicker(d time.Duration) Ticker
	AfterFunc(d time.Duration, f func())
}

// Ticker is time.Ticker with C as a method so Fake can implement it.
type Ticker interface {
	C() <-chan time.Time
	Stop()
}

type Real struct{}

func (Real) NewTicker(d time.Duration) Ticker    { return realTicker{time.NewTicker(d)} }
func (Real) AfterFunc(d time.Duration, f func()) { time.AfterFunc(d, f) }

type realTicker struct{ *time.Ticker }

func (t realTicker) C() <-chan time.Time { return t.Ticker.C }

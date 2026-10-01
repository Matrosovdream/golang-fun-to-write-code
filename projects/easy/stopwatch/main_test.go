package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"
)

func TestFmtClock(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{0, "00:00"},
		{-5 * time.Second, "00:00"},
		{42 * time.Second, "00:42"},
		{25 * time.Minute, "25:00"},
		{90*time.Minute + 5*time.Second, "1:30:05"},
		{1499 * time.Millisecond, "00:01"}, // rounds, not truncates
	}
	for _, tc := range tests {
		if got := fmtClock(tc.d); got != tc.want {
			t.Errorf("fmtClock(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
}

func TestCountdownFinishes(t *testing.T) {
	var buf bytes.Buffer
	start := time.Now()
	countdown(context.Background(), &buf, 50*time.Millisecond, 10*time.Millisecond)

	if elapsed := time.Since(start); elapsed < 50*time.Millisecond {
		t.Errorf("countdown returned after %v, before the deadline", elapsed)
	}
	if !strings.Contains(buf.String(), "time's up") {
		t.Errorf("missing completion message: %q", buf.String())
	}
}

func TestCountdownCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	var buf bytes.Buffer
	countdown(ctx, &buf, time.Hour, 10*time.Millisecond)

	if !strings.Contains(buf.String(), "cancelled") {
		t.Errorf("missing cancel message: %q", buf.String())
	}
}

func TestStopwatchLapsAndEOF(t *testing.T) {
	// Three Enter presses, then EOF ends the session.
	in := strings.NewReader("\n\n\n")
	var buf bytes.Buffer

	stopwatch(context.Background(), in, &buf)

	out := buf.String()
	for _, want := range []string{"lap 1:", "lap 2:", "lap 3:", "total"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

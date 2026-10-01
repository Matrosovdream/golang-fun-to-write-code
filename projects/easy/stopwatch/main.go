// stopwatch: press Enter for laps, Ctrl+C to finish.
// pomodoro:  stopwatch -pomodoro 25m — live countdown, then a bell.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"time"
)

func main() {
	pomodoro := flag.Duration("pomodoro", 0, "run a countdown instead (e.g. 25m, 90s)")
	flag.Parse()

	// Ctrl+C cancels this context — both modes end cleanly through it.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if *pomodoro > 0 {
		countdown(ctx, os.Stdout, *pomodoro, time.Second)
		return
	}
	stopwatch(ctx, os.Stdin, os.Stdout)
}

// stopwatch records a lap on every Enter press until ctx is cancelled.
// Reading stdin blocks, so a goroutine turns key presses into channel
// events — then one select merges "user pressed Enter" and "user hit
// Ctrl+C" into a single ordered stream.
func stopwatch(ctx context.Context, in io.Reader, w io.Writer) {
	fmt.Fprintln(w, "stopwatch running — Enter = lap, Ctrl+C = finish")
	start := time.Now()
	last := start

	laps := make(chan struct{})
	go func() {
		defer close(laps)
		sc := bufio.NewScanner(in)
		for sc.Scan() {
			select {
			case laps <- struct{}{}:
			case <-ctx.Done():
				return
			}
		}
	}()

	for lap := 1; ; lap++ {
		select {
		case _, ok := <-laps:
			if !ok {
				fmt.Fprintf(w, "total %s\n", fmtClock(time.Since(start)))
				return
			}
			now := time.Now()
			fmt.Fprintf(w, "lap %d: %s (total %s)\n",
				lap, fmtClock(now.Sub(last)), fmtClock(now.Sub(start)))
			last = now
		case <-ctx.Done():
			fmt.Fprintf(w, "\ntotal %s\n", fmtClock(time.Since(start)))
			return
		}
	}
}

// countdown repaints one terminal line (\r, no \n) every tick. A Ticker
// fires repeatedly; the deadline arrives once via ctx timeout — note the
// two different time tools and who stops what.
func countdown(ctx context.Context, w io.Writer, total, tick time.Duration) {
	deadline := time.NewTimer(total)
	defer deadline.Stop()
	ticker := time.NewTicker(tick)
	defer ticker.Stop()

	end := time.Now().Add(total)
	fmt.Fprintf(w, "\r%s remaining ", fmtClock(total))

	for {
		select {
		case <-ticker.C:
			fmt.Fprintf(w, "\r%s remaining ", fmtClock(time.Until(end).Round(tick)))
		case <-deadline.C:
			fmt.Fprintf(w, "\r00:00 — time's up!\a\n") // \a rings the terminal bell
			return
		case <-ctx.Done():
			fmt.Fprintf(w, "\rcancelled with %s left\n", fmtClock(time.Until(end).Round(tick)))
			return
		}
	}
}

// fmtClock renders mm:ss, or h:mm:ss once hours appear.
func fmtClock(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	d = d.Round(time.Second)
	h := d / time.Hour
	m := (d % time.Hour) / time.Minute
	s := (d % time.Minute) / time.Second
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%02d:%02d", m, s)
}

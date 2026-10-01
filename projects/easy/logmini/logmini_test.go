package logmini

import (
	"bytes"
	"strings"
	"sync"
	"testing"
	"time"
)

// pin replaces the clock so output is exactly predictable.
func pin(l *Logger) *Logger {
	fixed := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	l.now = func() time.Time { return fixed }
	return l
}

// The io.Writer payoff: log into a buffer, assert on the string.
func TestWritesFormattedLine(t *testing.T) {
	var buf bytes.Buffer
	l := pin(New(&buf, Debug))

	l.Infof("user %s logged in (%d tries)", "stan", 2)

	want := "12:00:00.000 INFO  user stan logged in (2 tries)\n"
	if buf.String() != want {
		t.Errorf("got %q, want %q", buf.String(), want)
	}
}

func TestLevelFiltering(t *testing.T) {
	var buf bytes.Buffer
	l := pin(New(&buf, Warn))

	l.Debugf("hidden")
	l.Infof("hidden too")
	l.Warnf("shown")
	l.Errorf("also shown")

	out := buf.String()
	if strings.Contains(out, "hidden") {
		t.Errorf("filtered levels leaked: %q", out)
	}
	if !strings.Contains(out, "WARN  shown") || !strings.Contains(out, "ERROR also shown") {
		t.Errorf("wanted levels missing: %q", out)
	}
}

func TestWithPrefix(t *testing.T) {
	var buf bytes.Buffer
	base := pin(New(&buf, Info))
	api := base.WithPrefix("api: ")

	api.Infof("ready")
	base.Infof("no prefix here")

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if !strings.Contains(lines[0], "api: ready") {
		t.Errorf("prefix missing: %q", lines[0])
	}
	if strings.Contains(lines[1], "api:") {
		t.Errorf("prefix leaked to parent: %q", lines[1])
	}
}

// Run with -race: parent and derived logger share one sink, so concurrent
// use must produce whole lines, never interleaved fragments.
func TestConcurrentLoggersKeepLinesWhole(t *testing.T) {
	var buf bytes.Buffer
	base := New(&buf, Info)
	derived := base.WithPrefix("worker: ")

	var wg sync.WaitGroup
	for range 50 {
		wg.Add(2)
		go func() { defer wg.Done(); base.Infof("base line") }()
		go func() { defer wg.Done(); derived.Infof("derived line") }()
	}
	wg.Wait()

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 100 {
		t.Fatalf("got %d lines, want 100", len(lines))
	}
	for _, line := range lines {
		if !strings.HasSuffix(line, "base line") && !strings.HasSuffix(line, "derived line") {
			t.Errorf("mangled line: %q", line)
		}
	}
}

func TestLevelString(t *testing.T) {
	if Debug.String() != "DEBUG" || Level(42).String() != "LEVEL(42)" {
		t.Error("Level.String broken")
	}
}

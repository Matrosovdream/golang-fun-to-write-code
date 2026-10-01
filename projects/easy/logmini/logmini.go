// Package logmini is a small leveled logger, built to show how a logging
// package is put together — including the tricks stdlib log and slog use.
//
// The single most important idea: the destination is an io.Writer.
// A file, stderr, a network socket, a bytes.Buffer in a test — the logger
// cannot tell the difference, and that's the whole point.
package logmini

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

type Level int

const (
	Debug Level = iota
	Info
	Warn
	Error
)

func (l Level) String() string {
	switch l {
	case Debug:
		return "DEBUG"
	case Info:
		return "INFO"
	case Warn:
		return "WARN"
	case Error:
		return "ERROR"
	default:
		return fmt.Sprintf("LEVEL(%d)", int(l))
	}
}

// sink is the shared write end. Derived loggers (WithPrefix) share one sink,
// so one mutex serializes all of them — two loggers with separate mutexes
// writing the same file would interleave mid-line.
type sink struct {
	mu  sync.Mutex
	out io.Writer
}

type Logger struct {
	s      *sink
	min    Level
	prefix string
	now    func() time.Time // injectable: tests pin the clock
}

func New(out io.Writer, min Level) *Logger {
	return &Logger{s: &sink{out: out}, min: min, now: time.Now}
}

// WithPrefix returns a derived logger; parent and child stay safe to use
// concurrently because they share the sink.
func (l *Logger) WithPrefix(prefix string) *Logger {
	clone := *l
	clone.prefix = prefix
	return &clone
}

func (l *Logger) Debugf(format string, args ...any) { l.log(Debug, format, args...) }
func (l *Logger) Infof(format string, args ...any)  { l.log(Info, format, args...) }
func (l *Logger) Warnf(format string, args ...any)  { l.log(Warn, format, args...) }
func (l *Logger) Errorf(format string, args ...any) { l.log(Error, format, args...) }

func (l *Logger) log(level Level, format string, args ...any) {
	if level < l.min {
		return // filtered before any formatting work happens
	}
	// Format BEFORE taking the lock: Sprintf can be slow, the write is fast.
	line := fmt.Sprintf("%s %-5s %s%s\n",
		l.now().Format("15:04:05.000"), level, l.prefix, fmt.Sprintf(format, args...))

	l.s.mu.Lock()
	defer l.s.mu.Unlock()
	_, _ = io.WriteString(l.s.out, line)
}

// --- package-level default, the way stdlib log does it ---

var std = New(os.Stderr, Info)

func Default() *Logger { return std }

func Debugf(format string, args ...any) { std.Debugf(format, args...) }
func Infof(format string, args ...any)  { std.Infof(format, args...) }
func Warnf(format string, args ...any)  { std.Warnf(format, args...) }
func Errorf(format string, args ...any) { std.Errorf(format, args...) }

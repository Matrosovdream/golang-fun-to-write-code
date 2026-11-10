package render

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"dagrun/internal/sched"
)

func TestLinesArePrefixedAndPadded(t *testing.T) {
	var out bytes.Buffer
	r := New(&out, []string{"a", "build"})
	w := r.Writer("a")

	// Writes split anywhere, including mid-line and across a blank line.
	for _, chunk := range []string{"hel", "lo\nwor", "ld\n\nlast", " bit"} {
		n, err := w.Write([]byte(chunk))
		require.NoError(t, err)
		require.Equal(t, len(chunk), n)
	}
	require.Equal(t, "[a    ] hello\n[a    ] world\n[a    ] \n", out.String(), "no output for the unfinished line yet")

	r.Observe(sched.Event{Kind: sched.Succeeded, Task: "a", Dur: 1234567 * time.Nanosecond})
	require.Equal(t, "[a    ] hello\n[a    ] world\n[a    ] \n[a    ] last bit\n[a    ] ok (1ms)\n", out.String(),
		"finishing a task flushes its unterminated last line")
}

func TestConcurrentWritersNeverInterleaveMidLine(t *testing.T) {
	var out bytes.Buffer
	r := New(&out, []string{"a", "b", "c"})
	var wg sync.WaitGroup
	for _, task := range []string{"a", "b", "c"} {
		w := r.Writer(task)
		wg.Go(func() {
			var all strings.Builder
			for i := range 300 {
				fmt.Fprintf(&all, "line %d %s\n", i, strings.Repeat(task, 40))
			}
			// Odd-sized chunks so lines straddle writes.
			s := all.String()
			for len(s) > 0 {
				n := min(len(s), 37)
				w.Write([]byte(s[:n]))
				s = s[n:]
			}
		})
	}
	wg.Wait()

	re := regexp.MustCompile(`^\[([abc])\] line \d+ (a{40}|b{40}|c{40})$`)
	lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
	require.Len(t, lines, 900)
	for _, l := range lines {
		m := re.FindStringSubmatch(l)
		require.NotNil(t, m, "garbled line %q", l)
		require.Equal(t, m[1], m[2][:1], "prefix and payload come from the same task: %q", l)
	}
}

func TestObserveAndSummary(t *testing.T) {
	var out bytes.Buffer
	r := New(&out, []string{"x"})
	r.Writer("x")
	for _, e := range []sched.Event{
		{Kind: sched.Started, Task: "x"},
		{Kind: sched.Failed, Task: "x", Dur: 2 * time.Second, Err: errors.New("exit status 1")},
		{Kind: sched.Skipped, Task: "x", Reason: `dependency "x" failed`},
	} {
		r.Observe(e)
	}
	require.Equal(t, "[x] started\n[x] FAILED (2s): exit status 1\n[x] skipped: dependency \"x\" failed\n", out.String())
	require.Equal(t, "0 ok, 1 failed, 1 skipped", r.Summary())
}

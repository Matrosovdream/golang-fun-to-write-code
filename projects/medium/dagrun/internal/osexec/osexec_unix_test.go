//go:build unix

package osexec

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"dagrun/internal/dag"
)

// The one test that spawns real processes: cancelling the context must kill
// the whole tree, including a grandchild that holds the output pipe open.
// Polite: SIGTERM must reach that grandchild (it leaves a marker as proof).
// Stubborn: everything ignores SIGTERM, so only the SIGKILL sweep can win.
func TestCancelKillsTheWholeProcessTree(t *testing.T) {
	tests := []struct {
		name, script string
		wantTerm     bool
		grace        time.Duration // generous when everyone cooperates, short when we must wait it out
	}{
		{"polite", `(trap 'echo > termfile; exit 0' TERM; sleep 30 & echo $! > pidfile; wait) & wait`, true, 5 * time.Second},
		{"ignores SIGTERM", `trap '' TERM; sleep 30 & echo $! > pidfile; wait`, false, 300 * time.Millisecond},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			var out bytes.Buffer
			errc := make(chan error, 1)
			go func() {
				task := dag.Task{Name: "t", Cmd: `echo "$GREETING from $(basename "$PWD")"; ` + tt.script, Env: map[string]string{"GREETING": "hi"}, Dir: dir}
				errc <- Executor{Grace: tt.grace}.Run(ctx, task, &out)
			}()

			var pid int
			require.Eventually(t, func() bool {
				b, err := os.ReadFile(filepath.Join(dir, "pidfile"))
				if err != nil {
					return false
				}
				pid, err = strconv.Atoi(strings.TrimSpace(string(b)))
				return err == nil
			}, 10*time.Second, 10*time.Millisecond, "grandchild never reported its pid")
			require.NoError(t, syscall.Kill(pid, 0), "grandchild should be alive before cancel")

			cancel()
			select {
			case err := <-errc:
				require.Error(t, err)
			case <-time.After(10 * time.Second):
				t.Fatal("Run did not return after cancel")
			}

			require.Eventually(t, func() bool { return syscall.Kill(pid, 0) == syscall.ESRCH },
				5*time.Second, 10*time.Millisecond, "grandchild sleep (pid %d) survived", pid)
			_, err := os.Stat(filepath.Join(dir, "termfile"))
			require.Equal(t, tt.wantTerm, err == nil, "grandchild received SIGTERM before any SIGKILL")
			require.Equal(t, "hi from "+filepath.Base(dir)+"\n", out.String(), "env and dir are applied")
		})
	}
}

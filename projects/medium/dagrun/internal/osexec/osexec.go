// Package osexec is the real sched.Executor: it runs a task's cmd with sh -c.
package osexec

import (
	"cmp"
	"context"
	"io"
	"maps"
	"os"
	"os/exec"
	"slices"
	"time"

	"dagrun/internal/dag"
)

type Executor struct {
	// Grace is how long a cancelled task may ignore SIGTERM before it is
	// killed. Zero means five seconds.
	Grace time.Duration
}

func (e Executor) Run(ctx context.Context, t dag.Task, out io.Writer) error {
	cmd := exec.CommandContext(ctx, "sh", "-c", t.Cmd)
	cmd.Dir = t.Dir
	cmd.Env = os.Environ()
	for _, k := range slices.Sorted(maps.Keys(t.Env)) {
		cmd.Env = append(cmd.Env, k+"="+t.Env[k])
	}
	// One writer for both streams: os/exec then copies them from a single
	// goroutine, so stdout and stderr lines keep their order.
	cmd.Stdout, cmd.Stderr = out, out
	// Backstop for ctx cancellation: Wait would otherwise block as long as any
	// grandchild holds the output pipe open.
	cmd.WaitDelay = cmp.Or(e.Grace, 5*time.Second)
	ownGroup(cmd)

	err := cmd.Run()
	if ctx.Err() != nil {
		// WaitDelay kills only the shell; sweep whatever ignored SIGTERM.
		killGroup(cmd)
	}
	return err
}

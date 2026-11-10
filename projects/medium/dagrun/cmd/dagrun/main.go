package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"dagrun/internal/dag"
	"dagrun/internal/osexec"
	"dagrun/internal/render"
	"dagrun/internal/sched"
)

func main() {
	// Ctrl+C cancels ctx; sched stops launching and osexec signals each
	// task's whole process group.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx, os.Args[1:], os.Stdout, os.Stderr, osexec.Executor{})
	stop()
	if err != nil {
		fmt.Fprintln(os.Stderr, "dagrun:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer, ex sched.Executor) error {
	fs := flag.NewFlagSet("dagrun", flag.ExitOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: dagrun [flags] [task...]   (flags go before task names)")
		fs.PrintDefaults()
	}
	var (
		file = fs.String("f", "dagrun.yaml", "task file")
		jobs = fs.Int("j", runtime.NumCPU(), "run at most this many tasks at once")
		keep = fs.Bool("k", false, "keep going: after a failure, still run tasks that do not depend on it")
		dry  = fs.Bool("dry-run", false, "print the plan level by level and exit")
		dot  = fs.Bool("dot", false, "print the graph as Graphviz and exit")
	)
	fs.Parse(args) // ExitOnError: never returns an error
	if *jobs < 1 {
		return errors.New("-j must be at least 1")
	}

	g, err := dag.Load(*file)
	if err != nil {
		return err
	}
	if g, err = g.Select(fs.Args()...); err != nil {
		return err
	}

	switch {
	case *dot:
		fmt.Fprint(stdout, g.Dot())
		return nil
	case *dry:
		for i, level := range g.Levels() {
			fmt.Fprintf(stdout, "level %d: %s\n", i+1, strings.Join(level, " "))
		}
		return nil
	}

	r := render.New(stdout, g.Names())
	s := sched.Scheduler{Exec: ex, Jobs: *jobs, KeepGoing: *keep, Output: r.Writer, Observe: r.Observe}
	start := time.Now()
	err = s.Run(ctx, g)
	fmt.Fprintf(stdout, "%s in %s\n", r.Summary(), time.Since(start).Round(time.Millisecond))
	return err
}

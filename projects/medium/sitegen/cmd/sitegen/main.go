package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"time"

	"sitegen/internal/serve"
	"sitegen/internal/site"
	"sitegen/internal/watch"
)

const usage = `usage:
  sitegen build [-o public] [-drafts] <site-dir>
  sitegen serve [-addr 127.0.0.1:8103] [-drafts] <site-dir>`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "sitegen:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout io.Writer) error {
	if len(args) > 0 {
		switch args[0] {
		case "build":
			return build(ctx, args[1:], stdout)
		case "serve":
			return serveCmd(ctx, args[1:], stdout)
		}
	}
	return errors.New(usage)
}

func build(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("build", flag.ExitOnError)
	outDir := fs.String("o", "public", "output directory")
	drafts := fs.Bool("drafts", false, "include posts marked draft: true")
	dir, err := parse(fs, args)
	if err != nil {
		return err
	}
	st, err := site.New(site.WithDrafts(*drafts)).Build(ctx, os.DirFS(dir), site.DirOutput(*outDir))
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "built %s -> %s: %d files written\n", dir, *outDir, st.Rendered)
	return nil
}

func serveCmd(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	addr := fs.String("addr", "127.0.0.1:8103", "listen address")
	drafts := fs.Bool("drafts", false, "include posts marked draft: true")
	dir, err := parse(fs, args)
	if err != nil {
		return err
	}

	srv := serve.New(os.DirFS(dir), site.New(site.WithDrafts(*drafts)), stdout)
	if err := srv.Rebuild(ctx); err != nil { // a broken site at startup is fatal; later breakage is not
		return err
	}
	paths, err := watch.Dir(ctx, dir)
	if err != nil {
		return err
	}
	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "serving %s on http://%s (ctrl+c to stop)\n", dir, ln.Addr())
	return srv.Run(ctx, ln, watch.Debounce(ctx, paths, 150*time.Millisecond, watch.RealClock{}))
}

// parse accepts the site directory before or after the flags, so both
// `sitegen build example -o out` and `sitegen build -o out example` work.
// (flag stops at the first non-flag argument; parsing twice gets past it.)
func parse(fs *flag.FlagSet, args []string) (string, error) {
	fs.Parse(args)
	if fs.NArg() == 0 {
		return "", errors.New(usage)
	}
	dir := fs.Arg(0)
	fs.Parse(fs.Args()[1:])
	if fs.NArg() > 0 {
		return "", errors.New(usage)
	}
	return dir, nil
}

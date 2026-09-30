package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/sync/errgroup"

	"metricsd/internal/server"
	"metricsd/internal/store"
)

func main() {
	var (
		tcpAddr  = flag.String("tcp", ":9125", "ingest address")
		httpAddr = flag.String("http", ":9126", "query/pprof address")
	)
	flag.Parse()
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))

	// GC tuning knobs to experiment with under load (see README):
	//   GOGC=200 ./metricsd        — GC less often, use more heap
	//   GOMEMLIMIT=256MiB ./metricsd — hard ceiling, GC harder near it
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	srv := server.New(store.New(), log)

	g, ctx := errgroup.WithContext(ctx)
	g.Go(func() error { return srv.ServeTCP(ctx, *tcpAddr) })
	g.Go(func() error { return srv.ServeHTTP(ctx, *httpAddr) })

	if err := g.Wait(); err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
	log.Info("bye")
}

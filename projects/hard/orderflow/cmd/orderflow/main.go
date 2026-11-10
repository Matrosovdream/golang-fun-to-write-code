// Command orderflow runs the orders, inventory and payments services and an
// embedded NATS JetStream server in one process. The services only ever talk
// to each other through the broker, so splitting them into separate
// processes means changing this file and nothing else.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"golang.org/x/sync/errgroup"

	"orderflow/internal/bus"
	"orderflow/internal/inventory"
	"orderflow/internal/orders"
	"orderflow/internal/payments"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8107", "orders REST listen address")
	dataDir := flag.String("data-dir", "", "directory for the SQLite files and the NATS store (default: a temp dir removed on exit)")
	failRate := flag.Float64("payment-fail-rate", 0.2, "probability that a charge is declined")
	debug := flag.Bool("debug", false, "log every processed message")
	flag.Parse()

	if *debug {
		slog.SetLogLoggerLevel(slog.LevelDebug)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, *addr, *dataDir, *failRate); err != nil {
		slog.Error("orderflow", "err", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, addr, dir string, failRate float64) error {
	if dir == "" {
		tmp, err := os.MkdirTemp("", "orderflow-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(tmp)
		dir = tmp
	}

	b, err := bus.Start(ctx, filepath.Join(dir, "nats"))
	if err != nil {
		return err
	}
	defer b.Close()

	ord, err := orders.New(filepath.Join(dir, "orders.db"), b.JS)
	if err != nil {
		return err
	}
	defer ord.Close()
	inv, err := inventory.New(filepath.Join(dir, "inventory.db"), b.JS, map[string]int{"widget": 12, "gadget": 8, "gizmo": 5})
	if err != nil {
		return err
	}
	defer inv.Close()
	pay, err := payments.New(filepath.Join(dir, "payments.db"), b.JS)
	if err != nil {
		return err
	}
	defer pay.Close()
	pay.Decline = payments.RandomDecline(failRate)

	mux := http.NewServeMux()
	ord.Routes(mux)
	mux.Handle("GET /stock", inv.StockHandler())
	srv := &http.Server{Addr: addr, Handler: mux}

	slog.Info("orderflow up", "http", addr, "nats", b.Server.ClientURL(), "data", dir, "payment_fail_rate", failRate)
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error { return ord.Run(gctx) })
	g.Go(func() error { return inv.Run(gctx) })
	g.Go(func() error { return pay.Run(gctx) })
	g.Go(func() error {
		if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("http: %w", err)
		}
		return nil
	})
	g.Go(func() error {
		<-gctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(sctx)
	})
	return g.Wait()
}

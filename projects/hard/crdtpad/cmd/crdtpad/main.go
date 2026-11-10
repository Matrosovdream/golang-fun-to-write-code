// Command crdtpad serves collaborative documents and the browser client.
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"crdtpad/internal/server"
)

func main() {
	addr := flag.String("addr", ":8106", "listen address")
	dir := flag.String("data", "data", "directory holding one oplog per document")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := run(*addr, *dir, log); err != nil {
		log.Error("crdtpad", "err", err)
		os.Exit(1)
	}
}

func run(addr, dir string, log *slog.Logger) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	app := server.New(ctx, dir, log)
	// No Read/WriteTimeout: they would cut long-lived WebSocket connections.
	srv := &http.Server{Addr: addr, Handler: app.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutdown)
	}()

	log.Info("listening", "addr", addr, "data", dir)
	err := srv.ListenAndServe()
	stop()
	app.Wait() // hubs close their oplogs once ctx is cancelled
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

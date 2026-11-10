package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"lbproxy/internal/config"
	"lbproxy/internal/proxy"
)

func main() {
	path := flag.String("config", "lb.yaml", "path to the YAML config")
	flag.Parse()
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := run(*path, log); err != nil {
		log.Error("lbproxy failed", "err", err)
		os.Exit(1)
	}
}

func run(path string, log *slog.Logger) error {
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	p, err := proxy.New(ctx, cfg, log)
	if err != nil {
		return err
	}

	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-hup:
				if err := p.ReloadFile(path); err != nil {
					log.Error("reload failed, keeping the running config", "err", err)
				} else {
					log.Info("config reloaded", "path", path)
				}
			}
		}
	}()

	srv := &http.Server{Addr: cfg.Listen, Handler: p.Handler(), ReadHeaderTimeout: 10 * time.Second}
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	log.Info("listening", "addr", cfg.Listen)

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	// Shutdown stops accepting and waits for in-flight requests to finish.
	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"shortlink/internal/handler"
	"shortlink/internal/repo"
	"shortlink/internal/service"
)

type config struct {
	Addr    string
	BaseURL string
	Backend string // "memory" or "sqlite"
	DBPath  string
}

func loadConfig() config {
	return config{
		Addr:    envOr("SHORTLINK_ADDR", ":8080"),
		BaseURL: envOr("SHORTLINK_BASE_URL", "http://localhost:8080"),
		Backend: envOr("SHORTLINK_BACKEND", "memory"),
		DBPath:  envOr("SHORTLINK_DB", "shortlink.db"),
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	// main only calls run: this keeps os.Exit out of the way of defers,
	// which do not run when os.Exit fires.
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg := loadConfig()
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	var (
		r   repo.Repo
		err error
	)
	switch cfg.Backend {
	case "memory":
		r = repo.NewMemory()
	case "sqlite":
		r, err = repo.NewSQLite(cfg.DBPath)
		if err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown backend %q", cfg.Backend)
	}

	svc := service.New(r)
	srv := &http.Server{
		Addr:    cfg.Addr,
		Handler: handler.New(svc, cfg.BaseURL, log),
		// Never ship an http.Server without timeouts: a slow client would
		// otherwise hold a connection (and a goroutine) forever.
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// ctx is cancelled on Ctrl+C / SIGTERM — the idiomatic shutdown trigger.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", cfg.Addr, "backend", cfg.Backend)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	// Graceful shutdown: stop accepting, let in-flight requests finish,
	// force-close after the timeout.
	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return nil
}

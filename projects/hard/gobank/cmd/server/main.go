package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"

	"gobank/internal/config"
	"gobank/internal/repo/postgres"
	"gobank/internal/service"
	httpapi "gobank/internal/transport/http"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}

func run() error {
	configPath := flag.String("config", "config.yaml", "path to config file")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}

	log, err := buildLogger(cfg.LogDev)
	if err != nil {
		return err
	}
	defer log.Sync() //nolint:errcheck // stdout sync errors are unactionable

	if err := postgres.Migrate(cfg.DBDSN); err != nil {
		return err
	}
	log.Info("migrations applied")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := postgres.Connect(ctx, cfg.DBDSN)
	if err != nil {
		return err
	}
	defer db.Close()

	// Composition root: every dependency is wired here, and only here.
	tokens := service.NewTokenManager(cfg.JWTSecret, cfg.TokenTTL)
	auth := service.NewAuth(postgres.NewUserRepo(db), tokens)
	bank := service.NewBank(postgres.NewAccountRepo(db), postgres.NewTransferRepo(db), db)

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           httpapi.NewRouter(auth, bank, tokens, log),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("listening", zap.String("addr", cfg.Addr))
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return nil
}

func buildLogger(dev bool) (*zap.Logger, error) {
	if dev {
		return zap.NewDevelopment() // human-readable, colored levels
	}
	return zap.NewProduction() // JSON, sampled, prod defaults
}

// jobqueue runs in three modes:
//
//	-mode all      API + workers in one process (default; -embedded adds miniredis)
//	-mode api      producer HTTP API only
//	-mode worker   worker fleet only
//
// Scale-out story: one `api`, N `worker` processes, real Redis.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/robfig/cron/v3"

	"jobqueue/internal/api"
	"jobqueue/internal/handlers"
	"jobqueue/internal/queue"
	"jobqueue/internal/worker"
)

func main() {
	var (
		mode      = flag.String("mode", "all", "all | api | worker")
		addr      = flag.String("addr", ":8087", "API listen address")
		redisAddr = flag.String("redis", "localhost:6379", "redis address")
		embedded  = flag.Bool("embedded", false, "run an in-process miniredis (no Docker needed)")
		workers   = flag.Int("workers", 4, "worker pool size")
	)
	flag.Parse()
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))

	if *embedded {
		mr, err := miniredis.Run()
		if err != nil {
			fatal(err)
		}
		defer mr.Close()
		*redisAddr = mr.Addr()
		log.Info("embedded miniredis", "addr", *redisAddr)
	}

	rdb := redis.NewClient(&redis.Options{Addr: *redisAddr})
	q := queue.New(rdb)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if *mode == "all" || *mode == "api" {
		srv := &http.Server{
			Addr:              *addr,
			Handler:           api.New(q, log),
			ReadHeaderTimeout: 5 * time.Second,
		}
		go func() {
			log.Info("api listening", "addr", *addr)
			if err := srv.ListenAndServe(); err != http.ErrServerClosed {
				fatal(err)
			}
		}()
		defer func() {
			shCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = srv.Shutdown(shCtx)
		}()
	}

	if *mode == "all" || *mode == "worker" {
		pool := worker.NewPool(q, log, *workers)
		handlers.RegisterAll(pool, log)

		// Recurring jobs: cron doesn't run work itself, it just enqueues —
		// workers stay the single execution path.
		c := cron.New()
		_, err := c.AddFunc("@every 30s", func() {
			_, err := q.Enqueue(context.Background(), "email",
				map[string]string{"to": "ops@example.com", "subject": "heartbeat"},
				queue.EnqueueOptions{Priority: "low"})
			if err != nil {
				log.Error("cron enqueue", "err", err)
			}
		})
		if err != nil {
			fatal(err)
		}
		c.Start()
		defer c.Stop()

		pool.Run(ctx) // blocks until signal, then drains
		return
	}

	<-ctx.Done() // api-only mode just waits for the signal
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "fatal:", err)
	os.Exit(1)
}

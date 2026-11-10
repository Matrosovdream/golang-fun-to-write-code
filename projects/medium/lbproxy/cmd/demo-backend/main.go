// demo-backend is a tiny upstream for trying lbproxy by hand: it answers with
// its own name, can be slowed down, and has a /healthz you can flip.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"time"
)

func main() {
	name := flag.String("name", "backend", "name this backend answers with")
	addr := flag.String("addr", "127.0.0.1:9101", "listen address")
	delay := flag.Duration("delay", 0, "artificial delay before each answer")
	flag.Parse()

	var down atomic.Bool
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		if down.Load() {
			http.Error(w, "down", http.StatusServiceUnavailable)
			return
		}
		fmt.Fprintln(w, "ok")
	})
	mux.HandleFunc("POST /healthz/{state}", func(w http.ResponseWriter, r *http.Request) {
		down.Store(r.PathValue("state") == "down")
		fmt.Fprintf(w, "%s: healthz down=%v\n", *name, down.Load())
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(*delay):
			fmt.Fprintf(w, "%s: %s %s\n", *name, r.Method, r.URL.Path)
		case <-r.Context().Done():
		}
	})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	srv := &http.Server{Addr: *addr, Handler: mux}
	go func() {
		<-ctx.Done()
		srv.Shutdown(context.Background())
	}()
	log.Printf("%s listening on %s", *name, *addr)
	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

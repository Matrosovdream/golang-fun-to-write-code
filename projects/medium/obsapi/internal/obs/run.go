package obs

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// Liveness asks "is it stuck?" (restart it); readiness asks "should it get
// traffic?" (stop routing, don't restart). A draining process is alive but not ready.
type Health struct{ draining atomic.Bool }

func (h *Health) Drain() { h.draining.Store(true) }

func (h *Health) Live(w http.ResponseWriter, _ *http.Request) {
	w.Write([]byte("ok\n"))
}

func (h *Health) Ready(w http.ResponseWriter, _ *http.Request) {
	if h.draining.Load() {
		http.Error(w, "draining", http.StatusServiceUnavailable)
		return
	}
	w.Write([]byte("ready\n"))
}

// Config holds the flags both binaries share.
type Config struct {
	Addr   string
	Sample float64
	Drain  time.Duration
}

func (c *Config) Bind(addr string) {
	flag.StringVar(&c.Addr, "addr", addr, "listen address")
	flag.Float64Var(&c.Sample, "sample", 1, "fraction of new traces to record; downstream services follow the caller's decision")
	flag.DurationVar(&c.Drain, "drain", 3*time.Second, "how long /readyz reports 503 before the listener closes")
}

// Run is the lifecycle both binaries share: exporter → provider → Obs → serve
// until SIGINT/SIGTERM → flush spans. build returns the service's handler.
func Run(service string, c Config, build func(*Obs) http.Handler) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	exp, err := NewExporter(ctx)
	if err != nil {
		return err
	}
	tp := NewTracerProvider(service, c.Sample, sdktrace.NewBatchSpanProcessor(exp, sdktrace.WithBatchTimeout(time.Second)))
	defer func() {
		// The batch processor buffers spans; without this flush the last
		// second of traces dies with the process.
		fctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := tp.Shutdown(fctx); err != nil {
			fmt.Fprintln(os.Stderr, "tracer shutdown:", err)
		}
	}()

	o := New(service, tp, os.Stdout)
	ln, err := net.Listen("tcp", c.Addr)
	if err != nil {
		return err
	}
	o.Log.Info("listening", "addr", ln.Addr().String())
	srv := &http.Server{Handler: build(o), ReadHeaderTimeout: 5 * time.Second}
	err = Serve(ctx, srv, ln, o.Health, c.Drain)
	o.Log.Info("stopped")
	return err
}

// Serve runs srv until ctx is cancelled, then: readiness fails, the load
// balancer notices during drain, and only then does Shutdown close the
// listener and wait for in-flight requests.
func Serve(ctx context.Context, srv *http.Server, ln net.Listener, h *Health, drain time.Duration) error {
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}

	h.Drain()
	time.Sleep(drain)

	// ctx is already cancelled; the grace period needs a fresh clock.
	sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	return srv.Shutdown(sctx)
}

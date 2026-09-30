// Package server: raw TCP ingest + HTTP for queries and observability.
package server

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"expvar"
	"log/slog"
	"net"
	"net/http"
	_ "net/http/pprof" // registers /debug/pprof/* on the DefaultServeMux
	"sync"
	"time"

	"metricsd/internal/parse"
	"metricsd/internal/store"
)

type Server struct {
	store *store.Store
	log   *slog.Logger

	mu    sync.Mutex
	conns map[net.Conn]struct{}
}

func New(s *store.Store, log *slog.Logger) *Server {
	srv := &Server{store: s, log: log, conns: make(map[net.Conn]struct{})}

	// expvar publishes live internals on /debug/vars — the zero-dependency
	// ancestor of Prometheus metrics.
	expvar.Publish("metricsd.ops", expvar.Func(func() any { return s.Ops() }))
	expvar.Publish("metricsd.conns", expvar.Func(func() any {
		srv.mu.Lock()
		defer srv.mu.Unlock()
		return len(srv.conns)
	}))
	return srv
}

// ServeTCP accepts ingest connections until ctx is cancelled.
func (s *Server) ServeTCP(ctx context.Context, addr string) error {
	var lc net.ListenConfig
	lis, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		return err
	}

	go func() {
		<-ctx.Done()
		lis.Close() // unblocks Accept
		s.mu.Lock()
		defer s.mu.Unlock()
		for c := range s.conns {
			c.Close() // unblocks in-flight Reads
		}
	}()

	s.log.Info("tcp ingest listening", "addr", addr)
	for {
		conn, err := lis.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		s.track(conn, true)
		go s.handleConn(ctx, conn)
	}
}

// handleConn reads newline-delimited events. Scanner.Bytes() returns a view
// into the scanner's reusable buffer, and parse.Parse keeps it borrowed —
// so the entire hot path (read → parse → apply on existing metrics) does
// not allocate.
func (s *Server) handleConn(ctx context.Context, conn net.Conn) {
	defer func() {
		s.track(conn, false)
		conn.Close()
	}()

	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 0, 64*1024), 64*1024)
	var malformed int64
	for sc.Scan() {
		ev, err := parse.Parse(sc.Bytes())
		if err != nil {
			malformed++
			continue // bad lines are counted, not fatal: ingest must not stop
		}
		s.store.Apply(ev)
	}
	if err := sc.Err(); err != nil && ctx.Err() == nil && !errors.Is(err, net.ErrClosed) {
		s.log.Warn("conn read", "err", err)
	}
	if malformed > 0 {
		s.log.Warn("malformed lines on connection", "count", malformed)
	}
}

// ServeHTTP exposes /metrics (snapshot), /debug/pprof/* and /debug/vars.
func (s *Server) ServeHTTP(ctx context.Context, addr string) error {
	// pprof and expvar register themselves on the DefaultServeMux — that is
	// why it is the handler here. On a public service, put these on a
	// separate internal-only port.
	http.HandleFunc("GET /metrics", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(s.store.Snapshot())
	})

	srv := &http.Server{Addr: addr, Handler: http.DefaultServeMux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		shCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(shCtx)
	}()

	s.log.Info("http listening", "addr", addr, "endpoints", "/metrics /debug/pprof /debug/vars")
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func (s *Server) track(c net.Conn, add bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if add {
		s.conns[c] = struct{}{}
	} else {
		delete(s.conns, c)
	}
}

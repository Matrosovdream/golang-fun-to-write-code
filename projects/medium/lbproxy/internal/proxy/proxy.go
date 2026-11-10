// Package proxy is the request path: route a request to a pool, pick a
// backend, forward with httputil.ReverseProxy, retry idempotent requests.
//
// The whole configuration is one immutable snapshot behind an atomic pointer.
// A reload builds a complete new snapshot and swaps it in; a request loads the
// pointer once and uses that snapshot to the end, so requests already in
// flight finish on the config they started with and no request ever sees a mix.
package proxy

import (
	"context"
	"errors"
	"expvar"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"sync/atomic"
	"time"

	"lbproxy/internal/config"
	"lbproxy/internal/lb"
)

// counters is published at /debug/vars. expvar names are process-global, so
// this is a package variable, not per-Proxy state.
var counters = expvar.NewMap("lbproxy")

var errBadStatus = errors.New("upstream returned a retryable status")

type Proxy struct {
	ctx       context.Context // bounds every snapshot's health checkers
	log       *slog.Logger
	transport *http.Transport
	snap      atomic.Pointer[snapshot]
}

func New(ctx context.Context, cfg *config.Config, log *slog.Logger) (*Proxy, error) {
	p := &Proxy{ctx: ctx, log: log, transport: &http.Transport{
		DialContext: (&net.Dialer{Timeout: 3 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		// The default of 2 idle connections per host makes a busy proxy
		// reconnect constantly: it talks to few hosts with many requests.
		MaxIdleConns:          256,
		MaxIdleConnsPerHost:   64,
		IdleConnTimeout:       90 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
	}}
	if err := p.Reload(cfg); err != nil {
		return nil, err
	}
	return p, nil
}

// Reload swaps in a snapshot built from cfg. If the build fails the old
// snapshot keeps serving.
func (p *Proxy) Reload(cfg *config.Config) error {
	s, err := p.build(cfg)
	if err != nil {
		return err
	}
	if old := p.snap.Swap(s); old != nil {
		old.stop() // its health checkers end; its in-flight requests carry on
	}
	counters.Add("config_loads", 1)
	return nil
}

// ReloadFile is what SIGHUP runs: parse, validate, swap. Any failure leaves
// the running config untouched.
func (p *Proxy) ReloadFile(path string) error {
	cfg, err := config.Load(path)
	if err == nil {
		err = p.Reload(cfg)
	}
	if err != nil {
		counters.Add("reload_failures", 1)
	}
	return err
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rt := p.snap.Load().match(r)
	if rt == nil {
		http.Error(w, "no route", http.StatusNotFound)
		return
	}
	p.forward(w, r, rt.pool)
}

// forward tries backends until one answers. Only GET/HEAD/OPTIONS without a
// body are retried, and always on a backend not yet tried: a POST that failed
// midway may already have been applied, and replaying it is not ours to decide.
func (p *Proxy) forward(w http.ResponseWriter, r *http.Request, pl *lb.Pool) {
	retryable := idempotent(r.Method) && (r.Body == nil || r.Body == http.NoBody)
	var tried []*lb.Backend
	for {
		b, candidates := pl.Pick(r, tried)
		if b == nil {
			break
		}
		tried = append(tried, b)
		final := !retryable || len(tried) > pl.Retries || candidates == 1

		written, failed := p.try(w, r, b, final)
		if failed && r.Context().Err() != nil {
			return // the client left; that says nothing about the backend
		}
		if pl.Report(b, !failed) {
			p.log.Warn("backend ejected", "pool", pl.Name, "backend", b.URL.Host)
		}
		if written {
			return
		}
		if final {
			counters.Add("upstream_errors", 1)
			http.Error(w, "bad gateway", http.StatusBadGateway)
			return
		}
		counters.Add("retries", 1)
	}
	counters.Add("no_backend", 1)
	http.Error(w, "no healthy backend", http.StatusServiceUnavailable)
}

// try sends r to b once. written says whether a response reached the client;
// failed says whether b should be blamed (transport error or 502/503/504).
// Unless final, a bad status is turned into an error from ModifyResponse so
// ReverseProxy discards the response unwritten and the caller can retry.
func (p *Proxy) try(w http.ResponseWriter, r *http.Request, b *lb.Backend, final bool) (written, failed bool) {
	b.Acquire()
	defer b.Release()
	var upstreamErr error
	status := 0
	rp := &httputil.ReverseProxy{
		Transport: p.transport,
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(b.URL)
			pr.SetXForwarded() // Rewrite sees the inbound X-Forwarded-* already stripped, so clients cannot spoof them
		},
		ModifyResponse: func(resp *http.Response) error {
			status = resp.StatusCode
			if !final && badStatus(status) {
				return errBadStatus
			}
			return nil
		},
		ErrorHandler: func(_ http.ResponseWriter, _ *http.Request, err error) { upstreamErr = err },
	}
	rp.ServeHTTP(w, r)
	return upstreamErr == nil, upstreamErr != nil || badStatus(status)
}

func idempotent(method string) bool {
	return method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions
}

func badStatus(code int) bool {
	return code == http.StatusBadGateway || code == http.StatusServiceUnavailable || code == http.StatusGatewayTimeout
}

// Package obs is the shared instrumentation for both services: one Obs per
// process bundles the three pillars (metrics, traces, logs) plus health.
package obs

import (
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
	"go.opentelemetry.io/otel/trace"
)

type Obs struct {
	Service string
	Log     *slog.Logger
	Metrics *Metrics
	Health  *Health
	TP      trace.TracerProvider
}

func New(service string, tp trace.TracerProvider, logw io.Writer) *Obs {
	return &Obs{Service: service, Log: NewLogger(logw, service), Metrics: NewMetrics(), Health: &Health{}, TP: tp}
}

// Handler wraps app: otelhttp (span, traceparent) → instrument (metrics, access
// log) → app mux. Tracing is outermost so the access log line runs inside the
// server span. Probes and /metrics are dispatched beside the stack so they
// create no spans and no RED series. (A ServeMux would do the dispatch too, but
// it sets r.Pattern = "/" before otelhttp reads it, so http.route would be "/".)
func (o *Obs) Handler(app http.Handler) http.Handler {
	metrics := promhttp.HandlerFor(o.Metrics.Registry, promhttp.HandlerOpts{})
	traced := otelhttp.NewHandler(o.instrument(app), o.Service,
		otelhttp.WithTracerProvider(o.TP),
		otelhttp.WithPropagators(Propagator),
	)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/metrics":
			metrics.ServeHTTP(w, r)
		case "/healthz":
			o.Health.Live(w, r)
		case "/readyz":
			o.Health.Ready(w, r)
		default:
			traced.ServeHTTP(w, r)
		}
	})
}

// Transport adds a client span and the traceparent header that links the callee's spans to ours.
func (o *Obs) Transport(base http.RoundTripper) http.RoundTripper {
	return otelhttp.NewTransport(base,
		otelhttp.WithTracerProvider(o.TP),
		otelhttp.WithPropagators(Propagator),
	)
}

// instrument must wrap the mux directly: the mux reports the matched pattern in
// r.Pattern, visible here only if nothing in between copied the request.
func (o *Obs) instrument(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		o.Metrics.inFlight.Inc()
		defer o.Metrics.inFlight.Dec()

		next.ServeHTTP(rec, r)

		elapsed := time.Since(start)
		method, route, code := methodLabel(r.Method), routeLabel(r.Pattern), strconv.Itoa(rec.status)
		o.Metrics.requests.WithLabelValues(method, route, code).Inc()
		o.Metrics.duration.WithLabelValues(method, route, code).Observe(elapsed.Seconds())
		if r.Pattern != "" {
			trace.SpanFromContext(r.Context()).SetAttributes(semconv.HTTPRoute(route))
		}

		level := slog.LevelInfo
		if rec.status >= 500 {
			level = slog.LevelError
		}
		// The raw path is fine in a log line; as a metric label it is not.
		o.Log.Log(r.Context(), level, "request",
			"method", r.Method, "path", r.URL.Path, "route", route,
			"status", rec.status, "duration_ms", float64(elapsed.Microseconds())/1000)
	})
}

// routeLabel maps "GET /products/{id}" to "/products/{id}". Everything no
// pattern matched (404s, 405s) shares one value: r.URL.Path here would mint a
// new time series per URL a scanner can invent.
func routeLabel(pattern string) string {
	if pattern == "" {
		return "unmatched"
	}
	if _, route, ok := strings.Cut(pattern, " "); ok {
		return route
	}
	return pattern
}

// methodLabel bounds the method label too: clients may send any token.
func methodLabel(m string) string {
	if slices.Contains([]string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"}, m) {
		return m
	}
	return "OTHER"
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

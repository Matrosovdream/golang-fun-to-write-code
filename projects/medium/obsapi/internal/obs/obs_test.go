package obs

import (
	"bytes"
	"context"
	"encoding/json"
	"math/rand/v2"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"obsapi/internal/obs/obstest"
)

// newStack returns a full obs stack around a small mux, plus the recorders.
func newStack(t *testing.T, routes func(*Obs, *http.ServeMux)) (*Obs, http.Handler, *tracetest.SpanRecorder, *bytes.Buffer) {
	t.Helper()
	rec := tracetest.NewSpanRecorder()
	logs := &bytes.Buffer{}
	o := New("test", NewTracerProvider("test", 1, rec), logs)
	mux := http.NewServeMux()
	routes(o, mux)
	return o, o.Handler(mux), rec, logs
}

func do(h http.Handler, method, target string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(method, target, nil))
	return w
}

func itemRoutes(o *Obs, mux *http.ServeMux) {
	mux.HandleFunc("GET /items/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})
	mux.HandleFunc("GET /boom", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})
}

func TestMetricsAreExactIncludingErrors(t *testing.T) {
	o, h, _, _ := newStack(t, itemRoutes)

	for i := range 7 {
		require.Equal(t, 200, do(h, "GET", "/items/"+string(rune('a'+i))).Code)
	}
	for range 3 {
		require.Equal(t, 500, do(h, "GET", "/boom").Code)
	}
	require.Equal(t, 404, do(h, "GET", "/nowhere").Code)
	require.Equal(t, 405, do(h, "POST", "/items/a").Code)

	reg := o.Metrics.Registry
	for _, c := range []struct {
		route, code string
		method      string
		want        float64
	}{
		{"/items/{id}", "200", "GET", 7}, // seven different ids, one series
		{"/boom", "500", "GET", 3},
		{"unmatched", "404", "GET", 1},
		{"unmatched", "405", "POST", 1},
	} {
		l := map[string]string{"method": c.method, "route": c.route, "code": c.code}
		require.Equal(t, c.want, obstest.Value(t, reg, "http_requests_total", l), "%v", l)
		require.Equal(t, c.want, obstest.Value(t, reg, "http_request_duration_seconds", l), "histogram count %v", l)
	}
	require.Equal(t, 4, testutil.CollectAndCount(o.Metrics.requests), "no stray series")
	require.Equal(t, 0.0, testutil.ToFloat64(o.Metrics.inFlight))
}

func TestInFlightGauge(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	o, h, _, _ := newStack(t, func(o *Obs, mux *http.ServeMux) {
		mux.HandleFunc("GET /slow", func(w http.ResponseWriter, r *http.Request) {
			close(started)
			<-release
		})
	})

	done := make(chan struct{})
	go func() { defer close(done); do(h, "GET", "/slow") }()
	<-started
	require.Equal(t, 1.0, testutil.ToFloat64(o.Metrics.inFlight))
	close(release)
	<-done
	require.Equal(t, 0.0, testutil.ToFloat64(o.Metrics.inFlight))
}

// The cardinality footgun: if the label were r.URL.Path, every URL a scanner
// invents would mint a new time series. Here 200 of them must mint none.
func TestUnknownPathsDoNotMintRouteLabels(t *testing.T) {
	o, h, _, _ := newStack(t, itemRoutes)
	do(h, "GET", "/items/1")
	do(h, "GET", "/nowhere")
	before := obstest.LabelValues(t, o.Metrics.Registry, "http_requests_total", "route")
	require.ElementsMatch(t, []string{"/items/{id}", "unmatched"}, before)

	rng := rand.New(rand.NewPCG(1, 2))
	methods := []string{"GET", "POST", "DELETE", "BREW", "X-RANDOM"}
	const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789-_"
	for range 200 {
		var path strings.Builder
		for range 1 + rng.IntN(4) {
			path.WriteByte('/')
			for range 1 + rng.IntN(12) {
				path.WriteByte(alphabet[rng.IntN(len(alphabet))])
			}
		}
		require.Equal(t, 404, do(h, methods[rng.IntN(len(methods))], path.String()).Code)
	}

	reg := o.Metrics.Registry
	after := obstest.LabelValues(t, reg, "http_requests_total", "route")
	require.Equal(t, before, after, "route label values grew")
	require.Equal(t, []string{"DELETE", "GET", "OTHER", "POST"},
		obstest.LabelValues(t, reg, "http_requests_total", "method"), "method label must be bounded too")
	require.Equal(t, 201.0, obstest.Value(t, reg, "http_requests_total", map[string]string{"route": "unmatched", "code": "404"}))
}

func TestLogLinesCarryTraceIdentity(t *testing.T) {
	_, h, rec, logs := newStack(t, func(o *Obs, mux *http.ServeMux) {
		mux.HandleFunc("GET /x", func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			o.Log.InfoContext(ctx, "with ctx")
			o.Log.With("k", "v").InfoContext(ctx, "with attrs")
			o.Log.Info("without ctx")
			cctx, child := o.TP.Tracer("t").Start(ctx, "child")
			o.Log.InfoContext(cctx, "in child")
			child.End()
		})
	})
	do(h, "GET", "/x")

	var server, child sdktrace.ReadOnlySpan
	for _, s := range rec.Ended() {
		switch s.Name() {
		case "child":
			child = s
		default:
			server = s
		}
	}
	require.NotNil(t, server)
	require.NotNil(t, child)

	lines := map[string]map[string]any{}
	for line := range bytes.SplitSeq(bytes.TrimSpace(logs.Bytes()), []byte("\n")) {
		var m map[string]any
		require.NoError(t, json.Unmarshal(line, &m), string(line))
		lines[m["msg"].(string)] = m
	}

	traceID, serverID := server.SpanContext().TraceID().String(), server.SpanContext().SpanID().String()
	for _, msg := range []string{"with ctx", "with attrs", "request"} {
		require.Equal(t, traceID, lines[msg]["trace_id"], msg)
		require.Equal(t, serverID, lines[msg]["span_id"], msg)
	}
	require.Equal(t, "v", lines["with attrs"]["k"])
	require.Equal(t, "test", lines["with ctx"]["service"])

	require.Equal(t, traceID, lines["in child"]["trace_id"])
	require.Equal(t, child.SpanContext().SpanID().String(), lines["in child"]["span_id"])

	require.NotContains(t, lines["without ctx"], "trace_id", "no context, no identity: use the *Context methods")
	require.Equal(t, "/x", lines["request"]["path"])
	require.Equal(t, "/x", lines["request"]["route"])
}

func TestLoggerIgnoresContextWithoutSpan(t *testing.T) {
	var buf bytes.Buffer
	NewLogger(&buf, "svc").InfoContext(context.Background(), "hi")
	require.NotContains(t, buf.String(), "trace_id")
	require.False(t, trace.SpanContextFromContext(context.Background()).IsValid())
}

func TestProbesAreNotTraced(t *testing.T) {
	_, h, rec, _ := newStack(t, itemRoutes)
	for _, p := range []string{"/healthz", "/readyz", "/metrics"} {
		require.Equal(t, 200, do(h, "GET", p).Code, p)
	}
	require.Empty(t, rec.Ended())
}

func TestHealthDrain(t *testing.T) {
	var h Health
	for _, tc := range []struct {
		draining        bool
		live, readiness int
	}{{false, 200, 200}, {true, 200, 503}} {
		if tc.draining {
			h.Drain()
		}
		require.Equal(t, tc.live, handlerCode(h.Live))
		require.Equal(t, tc.readiness, handlerCode(h.Ready))
	}
}

func handlerCode(f http.HandlerFunc) int {
	w := httptest.NewRecorder()
	f(w, httptest.NewRequest("GET", "/", nil))
	return w.Code
}

func TestReadinessFlipsWhenShutdownBeginsButLivenessStays(t *testing.T) {
	o, h, _, _ := newStack(t, itemRoutes)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	base := "http://" + ln.Addr().String()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, &http.Server{Handler: h}, ln, o.Health, 2*time.Second) }()

	code := func(path string) int {
		resp, err := http.Get(base + path)
		if err != nil {
			return -1
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	require.Equal(t, 200, code("/readyz"))
	require.Equal(t, 200, code("/healthz"))

	cancel() // SIGTERM arrives
	require.Eventually(t, func() bool { return code("/readyz") == 503 }, 5*time.Second, 5*time.Millisecond)
	require.Equal(t, 200, code("/healthz"), "alive while draining")
	require.Equal(t, 200, code("/items/1"), "still serving while draining")

	require.NoError(t, <-done)
	require.Equal(t, -1, code("/healthz"), "listener closed after the drain")
}

package gateway_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
	"go.opentelemetry.io/otel/trace"

	"obsapi/internal/catalog"
	"obsapi/internal/gateway"
	"obsapi/internal/obs"
	"obsapi/internal/obs/obstest"
)

// stack is the whole system in-process: two real services on two real
// listeners, each with its own tracer provider and span recorder, as if they
// were separate processes shipping spans to a collector.
type stack struct {
	gw, cat       *obs.Obs
	gwRec, catRec *tracetest.SpanRecorder
	gwSrv, catSrv *httptest.Server
	repo          *catalog.MemRepo
}

func newStack(t *testing.T, gwRatio, catRatio float64) *stack {
	t.Helper()
	s := &stack{gwRec: tracetest.NewSpanRecorder(), catRec: tracetest.NewSpanRecorder(), repo: catalog.NewMemRepo(catalog.Seed()...)}
	s.cat = obs.New("catalog", obs.NewTracerProvider("catalog", catRatio, s.catRec), io.Discard)
	s.catSrv = httptest.NewServer(catalog.NewHandler(s.cat, s.repo))
	s.gw = obs.New("gateway", obs.NewTracerProvider("gateway", gwRatio, s.gwRec), io.Discard)
	s.gwSrv = httptest.NewServer(gateway.NewHandler(s.gw, s.catSrv.URL))
	t.Cleanup(s.gwSrv.Close)
	t.Cleanup(s.catSrv.Close)
	return s
}

// get sends one request, then closes both servers. Close blocks until every
// handler has returned, which is also when every server span has ended, so
// the recorders can be read without sleeping or polling.
func (s *stack) get(t *testing.T, path string) (int, string) {
	t.Helper()
	resp, err := http.Get(s.gwSrv.URL + path)
	require.NoError(t, err)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	resp.Body.Close()
	s.gwSrv.Close()
	s.catSrv.Close()
	return resp.StatusCode, string(body)
}

func named(t *testing.T, rec *tracetest.SpanRecorder, name string) sdktrace.ReadOnlySpan {
	t.Helper()
	var found []sdktrace.ReadOnlySpan
	for _, s := range rec.Ended() {
		if s.Name() == name {
			found = append(found, s)
		}
	}
	require.Len(t, found, 1, "spans named %q", name)
	return found[0]
}

func TestOneTraceAcrossBothServices(t *testing.T) {
	s := newStack(t, 1, 1)
	code, body := s.get(t, "/products/p-1")
	require.Equal(t, 200, code)
	var p catalog.Product
	require.NoError(t, json.Unmarshal([]byte(body), &p))
	require.Equal(t, "p-1", p.ID)

	gwServer := named(t, s.gwRec, "GET /products/{id}")
	gwClient := named(t, s.gwRec, "HTTP GET")
	catServer := named(t, s.catRec, "GET /products/{id}")
	catSvc := named(t, s.catRec, "catalog.Get")
	catRepo := named(t, s.catRec, "repo.Get")
	require.Len(t, s.gwRec.Ended(), 2)
	require.Len(t, s.catRec.Ended(), 3)

	// One trace ID everywhere.
	all := []sdktrace.ReadOnlySpan{gwServer, gwClient, catServer, catSvc, catRepo}
	for _, sp := range all {
		require.Equal(t, gwServer.SpanContext().TraceID(), sp.SpanContext().TraceID(), sp.Name())
	}

	// The parent chain, across the process boundary and down through the layers.
	require.False(t, gwServer.Parent().IsValid(), "gateway server span is the root")
	require.Equal(t, gwServer.SpanContext().SpanID(), gwClient.Parent().SpanID())
	require.Equal(t, gwClient.SpanContext().SpanID(), catServer.Parent().SpanID(), "traceparent header linked the services")
	require.True(t, catServer.Parent().IsRemote())
	require.Equal(t, catServer.SpanContext().SpanID(), catSvc.Parent().SpanID())
	require.Equal(t, catSvc.SpanContext().SpanID(), catRepo.Parent().SpanID())

	require.Equal(t, trace.SpanKindServer, gwServer.SpanKind())
	require.Equal(t, trace.SpanKindClient, gwClient.SpanKind())
	require.Equal(t, trace.SpanKindServer, catServer.SpanKind())

	for _, sp := range s.gwRec.Ended() {
		require.Contains(t, sp.Resource().Attributes(), semconv.ServiceName("gateway"))
	}
	for _, sp := range s.catRec.Ended() {
		require.Contains(t, sp.Resource().Attributes(), semconv.ServiceName("catalog"))
	}
}

func TestStatusMappingAcrossTheHop(t *testing.T) {
	tests := []struct {
		name            string
		path            string
		failRate        float64
		want            int
		catCode, gwCode string
	}{
		{"ok", "/products/p-2", 0, 200, "200", "200"},
		{"not found passes through", "/products/nope", 0, 404, "404", "404"},
		{"catalog failure becomes 502", "/products/p-1", 1, 502, "500", "502"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newStack(t, 1, 1)
			s.repo.FailRate = tc.failRate
			code, _ := s.get(t, tc.path)
			require.Equal(t, tc.want, code)
			require.Equal(t, 1.0, obstest.Value(t, s.cat.Metrics.Registry, "http_requests_total", map[string]string{"code": tc.catCode}))
			require.Equal(t, 1.0, obstest.Value(t, s.gw.Metrics.Registry, "http_requests_total", map[string]string{"code": tc.gwCode}))
			require.Equal(t, 1.0, obstest.Value(t, s.gw.Metrics.Registry, "http_request_duration_seconds", map[string]string{"code": tc.gwCode}))
		})
	}
}

func TestCatalogFailureIsVisibleInTheTrace(t *testing.T) {
	s := newStack(t, 1, 1)
	s.repo.FailRate = 1
	s.get(t, "/products/p-1")

	require.Equal(t, codes.Error, named(t, s.catRec, "repo.Get").Status().Code)
	require.Equal(t, codes.Error, named(t, s.catRec, "GET /products/{id}").Status().Code)
	require.Equal(t, codes.Error, named(t, s.gwRec, "HTTP GET").Status().Code, "client span sees the 500")
	require.Equal(t, codes.Error, named(t, s.gwRec, "GET /products/{id}").Status().Code, "and so does the 502 the gateway returned")
}

// ParentBased sampling: the edge decides, everyone downstream follows. The
// catalog's own ratio only matters for requests that arrive without a parent.
func TestSamplingDecisionFollowsTheParent(t *testing.T) {
	tests := []struct {
		name                     string
		gwRatio, catRatio        float64
		wantGateway, wantCatalog int
	}{
		{"both sample", 1, 1, 2, 3},
		{"gateway drops, catalog follows", 0, 1, 0, 0},
		{"gateway keeps, catalog follows", 1, 0, 2, 3},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newStack(t, tc.gwRatio, tc.catRatio)
			code, _ := s.get(t, "/products/p-1")
			require.Equal(t, 200, code)
			require.Len(t, s.gwRec.Ended(), tc.wantGateway)
			require.Len(t, s.catRec.Ended(), tc.wantCatalog)
		})
	}
}

package catalog

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"obsapi/internal/obs"
)

func newHandler(t *testing.T, repo *MemRepo) (http.Handler, *tracetest.SpanRecorder) {
	t.Helper()
	rec := tracetest.NewSpanRecorder()
	o := obs.New("catalog", obs.NewTracerProvider("catalog", 1, rec), io.Discard)
	return NewHandler(o, repo), rec
}

func get(h http.Handler, target string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", target, nil))
	return w
}

func TestHandlerStatusCodes(t *testing.T) {
	tests := []struct {
		name     string
		target   string
		failRate float64
		want     int
		wantBody string
	}{
		{"found", "/products/p-1", 0, 200, `"Espresso mug"`},
		{"unknown id", "/products/nope", 0, 404, "product not found"},
		{"list", "/products", 0, 200, `"Desk mat"`},
		{"repo down", "/products/p-1", 1, 500, "internal error"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := NewMemRepo(Seed()...)
			repo.FailRate = tc.failRate
			h, _ := newHandler(t, repo)
			w := get(h, tc.target)
			require.Equal(t, tc.want, w.Code)
			require.Contains(t, w.Body.String(), tc.wantBody)
			require.NotContains(t, w.Body.String(), "repository unavailable", "internal error text must not leak")
		})
	}
}

func TestListIsSortedByID(t *testing.T) {
	h, _ := newHandler(t, NewMemRepo(Seed()...))
	var got []Product
	require.NoError(t, json.Unmarshal(get(h, "/products").Body.Bytes(), &got))
	require.Equal(t, Seed(), got)
}

func TestSpansPerLayer(t *testing.T) {
	repo := NewMemRepo(Seed()...)
	h, rec := newHandler(t, repo)

	get(h, "/products/nope")
	for _, s := range rec.Ended() {
		require.Equal(t, codes.Unset, s.Status().Code, "%s: a 404 is the caller's mistake", s.Name())
	}
	require.Equal(t, "rejected", spanNamed(t, rec, "repo.Get").Events()[0].Name)

	repo.FailRate = 1
	get(h, "/products/p-1")
	var failed []string
	for _, s := range rec.Ended() {
		if s.Status().Code == codes.Error {
			failed = append(failed, s.Name())
		}
	}
	require.ElementsMatch(t, []string{"repo.Get", "catalog.Get", "GET /products/{id}"}, failed)
	events := spanNamed(t, rec, "repo.Get").Events()
	require.Equal(t, "exception", events[len(events)-1].Name)
}

// spanNamed returns the most recent span with the given name.
func spanNamed(t *testing.T, rec *tracetest.SpanRecorder, name string) sdktrace.ReadOnlySpan {
	t.Helper()
	for _, s := range slices.Backward(rec.Ended()) {
		if s.Name() == name {
			return s
		}
	}
	require.FailNow(t, "no span named "+name)
	return nil
}

func TestCollectorReportsRepoCounters(t *testing.T) {
	repo := NewMemRepo(Seed()...)
	for _, id := range []string{"p-1", "p-2", "missing"} {
		repo.Get(context.Background(), id)
	}
	repo.List(context.Background())

	want := `
# HELP catalog_products Products currently in the catalog.
# TYPE catalog_products gauge
catalog_products 4
# HELP catalog_repo_reads_total Repository read calls, failed ones included.
# TYPE catalog_repo_reads_total counter
catalog_repo_reads_total 4
`
	require.NoError(t, testutil.CollectAndCompare(NewCollector(repo), strings.NewReader(want)))
}

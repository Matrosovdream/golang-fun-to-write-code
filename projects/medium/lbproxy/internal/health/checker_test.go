package health

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"lbproxy/internal/lb"
)

// probeTarget is a backend whose /healthz answer the test controls.
type probeTarget struct {
	srv  *httptest.Server
	code atomic.Int32
	hits atomic.Int32
	b    *lb.Backend
}

func newTarget(t *testing.T, pol lb.Policy) *probeTarget {
	pt := &probeTarget{}
	pt.code.Store(200)
	pt.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			http.NotFound(w, r)
			return
		}
		pt.hits.Add(1)
		w.WriteHeader(int(pt.code.Load()))
	}))
	t.Cleanup(pt.srv.Close)
	u, err := url.Parse(pt.srv.URL)
	require.NoError(t, err)
	pt.b = lb.NewBackend(u, 1, pol)
	return pt
}

func checker(timeout time.Duration, bs ...*lb.Backend) *Checker {
	return &Checker{Backends: bs, Path: "/healthz", Timeout: timeout, Client: http.DefaultClient,
		Log: slog.New(slog.DiscardHandler)}
}

func TestRoundDrivesTheStateMachine(t *testing.T) {
	pt := newTarget(t, lb.Policy{Rise: 2, Fall: 3})
	c := checker(5*time.Second, pt.b)
	ctx := context.Background()

	c.Round(ctx)
	require.True(t, pt.b.Healthy())

	pt.code.Store(503)
	c.Round(ctx)
	c.Round(ctx)
	require.True(t, pt.b.Healthy(), "two failures: still under Fall=3")
	c.Round(ctx)
	require.False(t, pt.b.Healthy(), "third failure takes it out")

	pt.code.Store(200)
	c.Round(ctx)
	require.False(t, pt.b.Healthy(), "one success: still under Rise=2")
	c.Round(ctx)
	require.True(t, pt.b.Healthy(), "second success brings it back")
}

func TestRoundProbesEveryBackendIndependently(t *testing.T) {
	pol := lb.Policy{Rise: 1, Fall: 1}
	good, bad := newTarget(t, pol), newTarget(t, pol)
	bad.code.Store(500)
	checker(5*time.Second, good.b, bad.b).Round(context.Background())
	require.True(t, good.b.Healthy())
	require.False(t, bad.b.Healthy())
	require.EqualValues(t, 1, good.hits.Load())
	require.EqualValues(t, 1, bad.hits.Load())
}

func TestRefusedConnectionAndTimeoutCountAsFailures(t *testing.T) {
	pol := lb.Policy{Rise: 1, Fall: 1}

	dead := newTarget(t, pol)
	dead.srv.Close()
	checker(5*time.Second, dead.b).Round(context.Background())
	require.False(t, dead.b.Healthy(), "connection refused")

	release := make(chan struct{})
	hung := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-release }))
	t.Cleanup(hung.Close)
	t.Cleanup(func() { close(release) })
	u, _ := url.Parse(hung.URL)
	b := lb.NewBackend(u, 1, pol)
	checker(50*time.Millisecond, b).Round(context.Background())
	require.False(t, b.Healthy(), "no answer within Timeout")
}

func TestRunProbesOnInjectedTicks(t *testing.T) {
	pt := newTarget(t, lb.Policy{Rise: 1, Fall: 1})
	pt.code.Store(500)
	tick := make(chan time.Time)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { checker(5*time.Second, pt.b).Run(ctx, tick); close(done) }()

	require.EqualValues(t, 0, pt.hits.Load(), "no tick, no probe")
	tick <- time.Now()
	require.Eventually(t, func() bool { return !pt.b.Healthy() }, 10*time.Second, time.Millisecond)

	cancel()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not stop after cancel")
	}
}

func TestCancelledRoundDoesNotBlameTheBackend(t *testing.T) {
	pt := newTarget(t, lb.Policy{Rise: 1, Fall: 1})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	checker(5*time.Second, pt.b).Round(ctx)
	require.True(t, pt.b.Healthy())
}

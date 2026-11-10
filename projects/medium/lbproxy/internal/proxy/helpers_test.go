package proxy

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"lbproxy/internal/config"
)

// upstream is an httptest backend that counts the requests it receives.
type upstream struct {
	*httptest.Server
	name string
	hits atomic.Int64
}

// newUpstream answers "<name>" unless h overrides it.
func newUpstream(t *testing.T, name string, h http.HandlerFunc) *upstream {
	u := &upstream{name: name}
	u.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			u.hits.Add(1)
		}
		if h != nil {
			h(w, r)
			return
		}
		fmt.Fprint(w, name)
	}))
	t.Cleanup(u.Close)
	return u
}

func failing(t *testing.T, name string) *upstream {
	return newUpstream(t, name, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, name+" is broken", http.StatusServiceUnavailable)
	})
}

// deadURL returns the address of a server that was just shut down: connections
// to it are refused immediately.
func deadURL(t *testing.T) string {
	s := httptest.NewServer(http.NotFoundHandler())
	s.Close()
	return s.URL
}

func urls(us ...*upstream) []string {
	out := make([]string, len(us))
	for i, u := range us {
		out[i] = u.URL
	}
	return out
}

// singlePool is a config with one catch-all route to a pool named p. extra is
// pre-indented pool YAML (four spaces).
func singlePool(t *testing.T, extra string, backendURLs ...string) *config.Config {
	var b strings.Builder
	b.WriteString("listen: 127.0.0.1:0\nroutes:\n  - pool: p\npools:\n  p:\n")
	b.WriteString(extra)
	b.WriteString("    backends:\n")
	for _, u := range backendURLs {
		fmt.Fprintf(&b, "      - url: %s\n", u)
	}
	cfg, err := config.Parse([]byte(b.String()))
	require.NoError(t, err)
	return cfg
}

type env struct {
	p   *Proxy
	srv *httptest.Server
	c   *http.Client
}

func start(t *testing.T, cfg *config.Config) *env {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	p, err := New(ctx, cfg, slog.New(slog.DiscardHandler))
	require.NoError(t, err)
	srv := httptest.NewServer(p.Handler())
	t.Cleanup(srv.Close)
	return &env{p, srv, srv.Client()}
}

func (e *env) do(t *testing.T, method, path string, body io.Reader, headers ...string) (int, string) {
	req, err := http.NewRequest(method, e.srv.URL+path, body)
	require.NoError(t, err)
	for i := 0; i < len(headers); i += 2 {
		if strings.EqualFold(headers[i], "Host") {
			req.Host = headers[i+1]
		} else {
			req.Header.Set(headers[i], headers[i+1])
		}
	}
	resp, err := e.c.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, string(b)
}

func (e *env) get(t *testing.T, path string, headers ...string) (int, string) {
	return e.do(t, http.MethodGet, path, nil, headers...)
}

func counter(name string) int64 {
	if v, ok := counters.Get(name).(interface{ Value() int64 }); ok {
		return v.Value()
	}
	return 0
}

func (e *env) statusJSON(t *testing.T) string {
	_, body := e.get(t, "/lb/status")
	return body
}

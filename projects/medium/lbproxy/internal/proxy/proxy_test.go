package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"lbproxy/internal/config"
)

func TestRoundRobinThroughProxy(t *testing.T) {
	a, b, c := newUpstream(t, "a", nil), newUpstream(t, "b", nil), newUpstream(t, "c", nil)
	e := start(t, singlePool(t, "", urls(a, b, c)...))

	var got []string
	for range 30 {
		code, body := e.get(t, "/")
		require.Equal(t, 200, code)
		got = append(got, body)
	}
	require.Equal(t, strings.Split("abcabcabcabcabcabcabcabcabcabc", ""), got)
	for _, u := range []*upstream{a, b, c} {
		require.EqualValues(t, 10, u.hits.Load(), u.name)
	}
}

func TestForwardingHeadersPathAndHost(t *testing.T) {
	u := newUpstream(t, "u", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s %s?%s host=%s xff=%s xfh=%s xfp=%s conn=%q",
			r.Method, r.URL.Path, r.URL.RawQuery, r.Host,
			r.Header.Get("X-Forwarded-For"), r.Header.Get("X-Forwarded-Host"), r.Header.Get("X-Forwarded-Proto"),
			r.Header.Get("Connection"))
	})
	e := start(t, singlePool(t, "", u.URL))

	// The client lies about X-Forwarded-For and sends a hop-by-hop header.
	_, body := e.get(t, "/a/b?x=1&y=2", "Host", "app.example", "X-Forwarded-For", "6.6.6.6", "Connection", "keep-alive, X-Secret", "X-Secret", "1")
	require.Contains(t, body, "GET /a/b?x=1&y=2 ")
	require.Contains(t, body, "host="+strings.TrimPrefix(u.URL, "http://"), "the backend sees its own host")
	require.Contains(t, body, "xff=127.0.0.1 ", "spoofed X-Forwarded-For is replaced, not appended to")
	require.Contains(t, body, "xfh=app.example")
	require.Contains(t, body, "xfp=http")
	require.NotContains(t, body, "6.6.6.6")
}

func TestRouting(t *testing.T) {
	api, v2, any, other := newUpstream(t, "api", nil), newUpstream(t, "v2", nil), newUpstream(t, "any", nil), newUpstream(t, "other", nil)
	cfg, err := config.Parse([]byte(fmt.Sprintf(`
listen: 127.0.0.1:0
routes:
  - {path_prefix: /api/, pool: api}
  - {path_prefix: /api/v2/, pool: v2}
  - {host: Other.Local, pool: other}
  - {path_prefix: /files/, pool: any}
pools:
  api: {backends: [{url: %s}]}
  v2: {backends: [{url: %s}]}
  any: {backends: [{url: %s}]}
  other: {backends: [{url: %s}]}
`, api.URL, v2.URL, any.URL, other.URL)))
	require.NoError(t, err)
	e := start(t, cfg)

	tests := []struct {
		path, host string
		code       int
		want       string
	}{
		{"/api/users", "", 200, "api"},
		{"/api/v2/users", "", 200, "v2"},
		{"/files/x", "", 200, "any"},
		{"/nothing", "", 404, "no route\n"},
		{"/api", "", 404, "no route\n"},
		{"/", "other.local", 200, "other"},
		{"/api/v2/x", "other.local:8102", 200, "other"},
	}
	for _, tt := range tests {
		t.Run(tt.path+"@"+tt.host, func(t *testing.T) {
			var hdr []string
			if tt.host != "" {
				hdr = []string{"Host", tt.host}
			}
			code, body := e.get(t, tt.path, hdr...)
			require.Equal(t, tt.code, code)
			require.Equal(t, tt.want, body)
		})
	}
}

func TestIdempotentRequestsRetryOnAnotherBackend(t *testing.T) {
	for _, method := range []string{"GET", "HEAD", "OPTIONS"} {
		t.Run(method, func(t *testing.T) {
			live := newUpstream(t, "live", nil)
			e := start(t, singlePool(t, "    retries: 1\n", deadURL(t), live.URL))
			before := counter("retries")
			for range 10 {
				code, _ := e.do(t, method, "/", nil)
				require.Equal(t, 200, code)
			}
			require.EqualValues(t, 10, live.hits.Load())
			require.Positive(t, counter("retries")-before)
		})
	}
}

func TestRetryOnUpstream503(t *testing.T) {
	bad, good := failing(t, "bad"), newUpstream(t, "good", nil)
	e := start(t, singlePool(t, "    retries: 1\n", urls(bad, good)...))
	for range 10 {
		code, body := e.get(t, "/")
		require.Equal(t, 200, code)
		require.Equal(t, "good", body, "the 503 never reaches the client")
	}
}

func TestNonIdempotentRequestsAreNeverRetried(t *testing.T) {
	t.Run("transport error on POST", func(t *testing.T) {
		live := newUpstream(t, "live", nil)
		e := start(t, singlePool(t, "    retries: 3\n    passive: {max_fails: 100}\n", deadURL(t), live.URL))
		var ok, bad int
		for range 10 {
			code, _ := e.do(t, "POST", "/", strings.NewReader("payload"))
			switch code {
			case 200:
				ok++
			case 502:
				bad++
			default:
				t.Fatalf("unexpected status %d", code)
			}
		}
		require.Equal(t, 5, bad, "every POST that picked the dead backend fails; none are replayed")
		require.Equal(t, 5, ok)
		require.EqualValues(t, 5, live.hits.Load(), "the live backend saw each of its own POSTs once and nothing replayed")
	})

	t.Run("503 on POST passes through", func(t *testing.T) {
		bad, good := failing(t, "bad"), newUpstream(t, "good", nil)
		e := start(t, singlePool(t, "    retries: 3\n    passive: {max_fails: 100}\n", urls(bad, good)...))
		for i := range 10 {
			code, body := e.do(t, "POST", "/", strings.NewReader("x"))
			if i%2 == 0 {
				require.Equal(t, 503, code)
				require.Equal(t, "bad is broken\n", body, "the backend's own answer, not ours")
			} else {
				require.Equal(t, 200, code)
			}
		}
		require.EqualValues(t, 5, bad.hits.Load())
		require.EqualValues(t, 5, good.hits.Load())
	})

	for _, method := range []string{"POST", "PUT", "DELETE", "PATCH"} {
		t.Run(method+" without a body", func(t *testing.T) {
			live := newUpstream(t, "live", nil)
			e := start(t, singlePool(t, "    retries: 3\n", deadURL(t), live.URL))
			code, _ := e.do(t, method, "/", nil) // round-robin starts at the dead backend
			require.Equal(t, 502, code)
			require.Zero(t, live.hits.Load())
		})
	}

	t.Run("GET with a body", func(t *testing.T) {
		live := newUpstream(t, "live", nil)
		e := start(t, singlePool(t, "    retries: 3\n    passive: {max_fails: 100}\n", deadURL(t), live.URL))
		code, _ := e.do(t, "GET", "/", strings.NewReader("a body cannot be replayed"))
		require.Equal(t, 502, code)
	})
}

func TestLastBadResponseIsPassedThrough(t *testing.T) {
	a, b := failing(t, "a"), failing(t, "b")
	e := start(t, singlePool(t, "    retries: 5\n", urls(a, b)...))
	code, body := e.get(t, "/")
	require.Equal(t, 503, code)
	require.Contains(t, body, "is broken", "the backend's 503, not a generic 502 from the proxy")
	require.EqualValues(t, 1, a.hits.Load())
	require.EqualValues(t, 1, b.hits.Load(), "each backend is tried once, never the same twice")
}

func TestRetriesAreCapped(t *testing.T) {
	us := []*upstream{failing(t, "a"), failing(t, "b"), failing(t, "c")}
	e := start(t, singlePool(t, "    retries: 1\n", urls(us...)...))
	e.get(t, "/")
	var total int64
	for _, u := range us {
		total += u.hits.Load()
	}
	require.EqualValues(t, 2, total, "1 attempt + 1 retry")
}

func TestNoHealthyBackend(t *testing.T) {
	e := start(t, singlePool(t, "    passive: {max_fails: 1, cooldown: 1h}\n", deadURL(t)))
	code, _ := e.get(t, "/")
	require.Equal(t, 502, code, "first request discovers the failure")
	code, body := e.get(t, "/")
	require.Equal(t, 503, code, "now the only backend is ejected")
	require.Equal(t, "no healthy backend\n", body)
}

func TestPassiveEjectionStopsTraffic(t *testing.T) {
	bad, good := failing(t, "bad"), newUpstream(t, "good", nil)
	e := start(t, singlePool(t, "    retries: 1\n    passive: {max_fails: 2, cooldown: 1h}\n", urls(bad, good)...))
	for range 30 {
		code, _ := e.get(t, "/")
		require.Equal(t, 200, code)
	}
	require.EqualValues(t, 2, bad.hits.Load(), "ejected after max_fails consecutive failures, then left alone")
	require.EqualValues(t, 30, good.hits.Load())

	_, status := e.get(t, "/lb/status")
	require.Contains(t, status, `"ejected": true`)
}

func TestLeastConnAvoidsTheBusyBackend(t *testing.T) {
	held := make(chan string, 1)
	release := make(chan struct{})
	h := func(name string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/hold" {
				held <- name
				<-release
			}
			fmt.Fprint(w, name)
		}
	}
	a, b := newUpstream(t, "a", h("a")), newUpstream(t, "b", h("b"))
	e := start(t, singlePool(t, "    strategy: least_conn\n", urls(a, b)...))

	var wg sync.WaitGroup
	wg.Go(func() { e.get(t, "/hold") })
	busy := <-held
	idle := map[string]string{"a": "b", "b": "a"}[busy]
	for range 10 {
		_, body := e.get(t, "/")
		require.Equal(t, idle, body)
	}
	close(release)
	wg.Wait()
}

func TestConsistentHashIsStickyThroughProxy(t *testing.T) {
	us := []*upstream{newUpstream(t, "a", nil), newUpstream(t, "b", nil), newUpstream(t, "c", nil)}
	e := start(t, singlePool(t, "    strategy: consistent_hash\n    hash_key: header:X-User\n", urls(us...)...))
	seen := map[string]bool{}
	for i := range 30 {
		user := fmt.Sprintf("user-%d", i)
		_, first := e.get(t, "/", "X-User", user)
		for range 3 {
			_, again := e.get(t, "/", "X-User", user)
			require.Equal(t, first, again, user)
		}
		seen[first] = true
	}
	require.Len(t, seen, 3, "30 users should land on every backend")
}

func TestWeightedRoundRobinThroughProxy(t *testing.T) {
	a, b := newUpstream(t, "a", nil), newUpstream(t, "b", nil)
	cfg := singlePool(t, "    strategy: weighted_round_robin\n", a.URL, b.URL)
	p := cfg.Pools["p"]
	p.Backends[0].Weight = 3
	e := start(t, cfg)
	for range 40 {
		e.get(t, "/")
	}
	require.EqualValues(t, 30, a.hits.Load())
	require.EqualValues(t, 10, b.hits.Load())
}

func TestActiveHealthCheckRemovesAndRestoresBackend(t *testing.T) {
	var down atomic.Bool
	flaky := newUpstream(t, "flaky", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" && down.Load() {
			http.Error(w, "down", http.StatusServiceUnavailable)
			return
		}
		fmt.Fprint(w, "flaky")
	})
	steady := newUpstream(t, "steady", nil)
	e := start(t, singlePool(t, "    health: {interval: 250ms, timeout: 250ms, rise: 2, fall: 2}\n", urls(flaky, steady)...))

	healthy := func() bool {
		var st struct {
			Pools []struct {
				Backends []struct{ Healthy bool }
			}
		}
		_, body := e.get(t, "/lb/status")
		require.NoError(t, json.Unmarshal([]byte(body), &st))
		return st.Pools[0].Backends[0].Healthy
	}

	down.Store(true)
	require.Eventually(t, func() bool { return !healthy() }, 20*time.Second, 5*time.Millisecond)
	for range 20 {
		_, body := e.get(t, "/")
		require.Equal(t, "steady", body, "an unhealthy backend gets no traffic")
	}

	down.Store(false)
	require.Eventually(t, healthy, 20*time.Second, 5*time.Millisecond)
	seen := map[string]bool{}
	for range 10 {
		_, body := e.get(t, "/")
		seen[body] = true
	}
	require.Len(t, seen, 2, "recovered backend is back in rotation")
}

func TestClientCancelIsNotABackendFailure(t *testing.T) {
	started := make(chan struct{})
	slow := newUpstream(t, "slow", func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	})
	e := start(t, singlePool(t, "    retries: 2\n    passive: {max_fails: 1}\n", slow.URL))

	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", e.srv.URL+"/", nil)
	done := make(chan error, 1)
	go func() { _, err := e.c.Do(req); done <- err }()
	<-started
	cancel()
	require.Error(t, <-done)

	idle := func() bool { return !strings.Contains(e.statusJSON(t), `"inflight": 1`) }
	require.Eventually(t, idle, 10*time.Second, 5*time.Millisecond)
	require.Contains(t, e.statusJSON(t), `"failures": 0`)
	require.Contains(t, e.statusJSON(t), `"ejected": false`)
}

func TestStreamingResponsesAreFlushedThroughTheDecorator(t *testing.T) {
	finish := make(chan struct{})
	sse := newUpstream(t, "sse", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: first\n\n")
		w.(http.Flusher).Flush()
		<-finish
	})
	e := start(t, singlePool(t, "", sse.URL))
	t.Cleanup(func() { close(finish) }) // runs before the servers close, or Close would wait on the open stream

	resp, err := e.c.Get(e.srv.URL + "/")
	require.NoError(t, err)
	defer resp.Body.Close()
	buf := make([]byte, len("data: first\n\n"))
	_, err = io.ReadFull(resp.Body, buf)
	require.NoError(t, err, "first event arrives while the backend is still holding the stream open")
	require.Equal(t, "data: first\n\n", string(buf))
}

func TestStatusAndExpvar(t *testing.T) {
	a, b := newUpstream(t, "a", nil), newUpstream(t, "b", nil)
	e := start(t, singlePool(t, "    health: {interval: 1h}\n", urls(a, b)...))
	before := counter("requests")
	for range 8 {
		e.get(t, "/")
	}

	var st status
	code, body := e.get(t, "/lb/status")
	require.Equal(t, 200, code)
	require.NoError(t, json.Unmarshal([]byte(body), &st))
	require.Len(t, st.Pools, 1)
	pool := st.Pools[0]
	require.Equal(t, "p", pool.Name)
	require.Equal(t, "round_robin", pool.Strategy)
	require.Len(t, pool.Backends, 2)
	for i, bs := range pool.Backends {
		require.Equal(t, []string{a.URL, b.URL}[i], bs.URL)
		require.True(t, bs.Healthy)
		require.False(t, bs.Ejected)
		require.Zero(t, bs.Inflight)
		require.EqualValues(t, 4, bs.Requests)
		require.Zero(t, bs.Failures)
	}
	require.EqualValues(t, 8, st.Counters["requests"]-before)

	_, vars := e.get(t, "/debug/vars")
	var parsed map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(vars), &parsed))
	require.Contains(t, string(parsed["lbproxy"]), `"requests"`)
}

func TestOnlyTheExactAdminPathsAreReserved(t *testing.T) {
	u := newUpstream(t, "u", nil)
	e := start(t, singlePool(t, "", u.URL))
	_, body := e.get(t, "/lb/other")
	require.Equal(t, "u", body)
}

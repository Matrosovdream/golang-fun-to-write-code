package proxy

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"lbproxy/internal/config"
)

func TestInFlightRequestFinishesOnTheConfigItStartedWith(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	oldUp := newUpstream(t, "old", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/slow" {
			close(started)
			<-release
		}
		fmt.Fprint(w, "old")
	})
	newUp := newUpstream(t, "new", nil)
	e := start(t, singlePool(t, "", oldUp.URL))

	var body string
	var wg sync.WaitGroup
	wg.Go(func() { _, body = e.get(t, "/slow") })
	<-started

	require.NoError(t, e.p.Reload(singlePool(t, "", newUp.URL)))
	_, fresh := e.get(t, "/")
	require.Equal(t, "new", fresh, "new requests use the new config at once")

	close(release)
	wg.Wait()
	require.Equal(t, "old", body, "the in-flight request was not dropped or redirected")
}

func TestInvalidReloadKeepsTheOldConfigRunning(t *testing.T) {
	oldUp, newUp := newUpstream(t, "old", nil), newUpstream(t, "new", nil)
	path := filepath.Join(t.TempDir(), "lb.yaml")
	write := func(content string) { require.NoError(t, os.WriteFile(path, []byte(content), 0o644)) }
	valid := func(u *upstream) string {
		return fmt.Sprintf("listen: 127.0.0.1:0\nroutes: [{pool: p}]\npools: {p: {backends: [{url: %s}]}}\n", u.URL)
	}

	write(valid(oldUp))
	cfg, err := config.Load(path)
	require.NoError(t, err)
	e := start(t, cfg)

	bad := map[string]string{
		"not yaml":        "listen: [",
		"unknown pool":    "listen: :1\nroutes: [{pool: nope}]\npools: {p: {backends: [{url: 'http://a:1'}]}}\n",
		"unknown field":   valid(newUp) + "surprise: true\n",
		"no backends":     "listen: :1\nroutes: [{pool: p}]\npools: {p: {backends: []}}\n",
		"empty file":      "",
		"truncated write": valid(newUp)[:40],
	}
	loads, failures := counter("config_loads"), counter("reload_failures")
	for name, content := range bad {
		write(content)
		require.Error(t, e.p.ReloadFile(path), name)
		_, body := e.get(t, "/")
		require.Equal(t, "old", body, name)
	}
	require.NoError(t, os.Remove(path))
	require.Error(t, e.p.ReloadFile(path), "missing file")
	_, body := e.get(t, "/")
	require.Equal(t, "old", body)
	require.EqualValues(t, len(bad)+1, counter("reload_failures")-failures)
	require.Equal(t, loads, counter("config_loads"), "nothing was swapped")

	// A config that passes no validation at all (built by hand) is refused by the snapshot build.
	raw := &config.Config{Routes: []config.Route{{Prefix: "/", Pool: "p"}},
		Pools: map[string]config.Pool{"p": {Strategy: "bogus", Backends: []config.Backend{{URL: newUp.URL, Weight: 1}}}}}
	require.ErrorContains(t, e.p.Reload(raw), "unknown strategy")
	_, body = e.get(t, "/")
	require.Equal(t, "old", body)

	write(valid(newUp))
	require.NoError(t, e.p.ReloadFile(path))
	_, body = e.get(t, "/")
	require.Equal(t, "new", body, "a valid file takes effect")
}

// The race detector is the assertion here: requests read the snapshot pointer
// and its pools while reloads build, swap and stop snapshots (and their
// health checkers) as fast as they can. Every request must still succeed.
func TestReloadUnderConcurrentLoad(t *testing.T) {
	a, b := newUpstream(t, "a", nil), newUpstream(t, "b", nil)
	cfgA := func() *config.Config {
		return singlePool(t, "    retries: 1\n", a.URL)
	}
	cfgB := func() *config.Config {
		c := singlePool(t, "    strategy: weighted_round_robin\n    retries: 1\n    health: {interval: 5ms, timeout: 5ms, fall: 1000}\n", a.URL, b.URL)
		c.Pools["p"].Backends[0].Weight = 2
		return c
	}
	e := start(t, cfgA())

	var stop atomic.Bool
	var requests, failures atomic.Int64
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for !stop.Load() {
				resp, err := e.c.Get(e.srv.URL + "/")
				requests.Add(1)
				if err != nil {
					failures.Add(1)
					continue
				}
				resp.Body.Close()
				if resp.StatusCode != 200 {
					failures.Add(1)
				}
			}
		})
	}

	for i := range 200 {
		cfg := cfgA()
		if i%2 == 1 {
			cfg = cfgB()
		}
		require.NoError(t, e.p.Reload(cfg))
		for target := requests.Load() + 2; requests.Load() < target; { // let traffic flow between reloads
			runtime.Gosched()
		}
	}
	stop.Store(true)
	wg.Wait()

	require.Zero(t, failures.Load(), "no request may fail because a reload happened")
	require.Positive(t, requests.Load())
	require.Positive(t, b.hits.Load(), "the snapshots that include b were live while requests ran")
	t.Logf("%d requests across 200 reloads, %d failures", requests.Load(), failures.Load())
}

func TestReloadStopsTheOldHealthCheckers(t *testing.T) {
	u := newUpstream(t, "u", nil)
	e := start(t, singlePool(t, "    health: {interval: 1h}\n", u.URL))
	base := runtime.NumGoroutine()
	for range 50 {
		require.NoError(t, e.p.Reload(singlePool(t, "    health: {interval: 1h}\n", u.URL)))
	}
	require.Eventually(t, func() bool { return runtime.NumGoroutine() <= base+5 }, 10*time.Second, 10*time.Millisecond,
		"each reload must cancel the previous snapshot's checker goroutine")
}

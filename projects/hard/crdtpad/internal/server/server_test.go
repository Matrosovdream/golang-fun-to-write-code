package server

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/stretchr/testify/require"

	"crdtpad/internal/client"
	"crdtpad/internal/rga"
	"crdtpad/internal/wire"
)

// startServer runs a real server on 127.0.0.1:0 and returns its base URL and
// an idempotent stop that also waits for the oplogs to be closed.
func startServer(t *testing.T, dir string) (string, func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	s := New(ctx, dir, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ts := httptest.NewServer(s.Handler())
	stop := sync.OnceFunc(func() { cancel(); s.Wait(); ts.Close() })
	t.Cleanup(stop)
	return ts.URL, stop
}

func connect(t *testing.T, base, doc, replica string) *client.Client {
	t.Helper()
	c := client.New(replica, base, doc)
	require.NoError(t, c.Connect(t.Context()))
	t.Cleanup(c.Disconnect)
	return c
}

func serverText(t *testing.T, base, doc string) string {
	t.Helper()
	resp, err := http.Get(base + "/doc/" + doc)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	b, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return string(b)
}

// waitConverged waits until every client has applied every client's ops.
// The target is read after the editors stopped, so it is exact. Nothing here
// sleeps for a guessed duration: it polls a condition with a generous bound.
func waitConverged(t *testing.T, clients map[string]*client.Client) {
	t.Helper()
	want := rga.Version{}
	for name, c := range clients {
		if seq := c.Version()[name]; seq > 0 {
			want[name] = seq
		}
	}
	require.Eventually(t, func() bool {
		for _, c := range clients {
			if !maps.Equal(c.Version(), want) {
				return false
			}
		}
		return true
	}, 30*time.Second, 10*time.Millisecond, "clients did not receive every op")
	for name, c := range clients {
		require.Equal(t, clients["alice"].Text(), c.Text(), "%s diverged", name)
	}
}

func randomEdits(t *testing.T, c *client.Client, rng *rand.Rand, letters string, n int, deletes bool) {
	t.Helper()
	for range n {
		length := c.Len()
		if deletes && length > 0 && rng.IntN(3) == 0 {
			require.NoError(t, c.Delete(rng.IntN(length)))
			continue
		}
		require.NoError(t, c.Insert(rng.IntN(length+1), rune(letters[rng.IntN(len(letters))])))
	}
}

func TestThreeClientsConverge(t *testing.T) {
	base, _ := startServer(t, t.TempDir())
	alphabets := map[string]string{"alice": "abc", "bob": "123", "carol": "XYZ"}
	clients := map[string]*client.Client{}
	for name := range alphabets {
		clients[name] = connect(t, base, "pad", name)
	}

	edit := func(phase uint64, deletes bool) {
		var wg sync.WaitGroup
		for name, c := range clients {
			wg.Go(func() {
				rng := rand.New(rand.NewPCG(phase, uint64(name[0])))
				randomEdits(t, c, rng, alphabets[name], 80, deletes)
			})
		}
		wg.Wait()
		waitConverged(t, clients)
	}

	// Phase 1: concurrent inserts only, so the exact set of characters is known.
	edit(1, false)
	text := clients["alice"].Text()
	for name, alphabet := range alphabets {
		mine := 0
		for _, r := range text {
			if strings.ContainsRune(alphabet, r) {
				mine++
			}
		}
		require.Equal(t, 80, mine, "characters typed by %s", name)
	}
	require.Equal(t, text, serverText(t, base, "pad"))

	// Phase 2: concurrent inserts and deletes.
	edit(2, true)
	require.Equal(t, clients["bob"].Text(), serverText(t, base, "pad"))
}

// Carol goes offline, edits locally, and reconnects while Alice and Bob kept
// typing. None of the edits touch the same spot, so the merged text is known.
func TestOfflineEditsMergeOnReconnect(t *testing.T) {
	base, _ := startServer(t, t.TempDir())
	clients := map[string]*client.Client{}
	for _, name := range []string{"alice", "bob", "carol"} {
		clients[name] = connect(t, base, "pad", name)
	}
	typeString := func(c *client.Client, pos int, s string) {
		for i, r := range s {
			require.NoError(t, c.Insert(pos+i, r))
		}
	}
	typeString(clients["alice"], 0, "hello world")
	waitConverged(t, clients)

	clients["carol"].Disconnect()
	typeString(clients["carol"], 0, "XYZ")
	require.NoError(t, clients["carol"].Delete(8)) // the space
	require.Equal(t, "XYZhelloworld", clients["carol"].Text(), "offline edits apply locally at once")
	typeString(clients["bob"], 5, "123")
	typeString(clients["alice"], 11, "abc")
	require.Eventually(t, func() bool { return clients["alice"].Text() == clients["bob"].Text() }, 30*time.Second, 10*time.Millisecond)
	require.NotContains(t, clients["alice"].Text(), "XYZ", "carol is really offline")

	require.NoError(t, clients["carol"].Connect(t.Context()))
	waitConverged(t, clients)
	require.Equal(t, "XYZhello123worldabc", clients["alice"].Text())
	require.Equal(t, clients["alice"].Text(), serverText(t, base, "pad"))
}

func TestRestartKeepsTheDocument(t *testing.T) {
	dir := t.TempDir()
	base, stop := startServer(t, dir)
	alice := connect(t, base, "pad", "alice")
	for i, r := range "persist me" {
		require.NoError(t, alice.Insert(i, r))
	}
	require.NoError(t, alice.Delete(0))
	require.Eventually(t, func() bool { return serverText(t, base, "pad") == "ersist me" }, 30*time.Second, 10*time.Millisecond)
	alice.Disconnect()
	stop()

	base, _ = startServer(t, dir)
	require.Equal(t, "ersist me", serverText(t, base, "pad"))

	bob := connect(t, base, "pad", "bob")
	require.Eventually(t, func() bool { return bob.Text() == "ersist me" }, 30*time.Second, 10*time.Millisecond)
	require.NoError(t, bob.Insert(0, 'P'))
	require.Eventually(t, func() bool { return serverText(t, base, "pad") == "Persist me" }, 30*time.Second, 10*time.Millisecond)
}

func TestDocumentsAreIndependent(t *testing.T) {
	base, _ := startServer(t, t.TempDir())
	a, b := connect(t, base, "one", "alice"), connect(t, base, "two", "bob")
	require.NoError(t, a.Insert(0, 'A'))
	require.NoError(t, b.Insert(0, 'B'))
	require.Eventually(t, func() bool {
		return serverText(t, base, "one") == "A" && serverText(t, base, "two") == "B"
	}, 30*time.Second, 10*time.Millisecond)
}

func TestRejectsBadRequests(t *testing.T) {
	base, _ := startServer(t, t.TempDir())
	for _, path := range []string{"/doc/has%20space", "/doc/" + strings.Repeat("x", 65), "/ws?doc=..%2Fetc", "/ws"} {
		resp, err := http.Get(base + path)
		require.NoError(t, err)
		resp.Body.Close()
		require.Equal(t, http.StatusBadRequest, resp.StatusCode, path)
	}

	resp, err := http.Get(base + "/")
	require.NoError(t, err)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Contains(t, string(body), "<textarea")
}

func TestFirstMessageMustBeHello(t *testing.T) {
	base, _ := startServer(t, t.TempDir())
	for _, first := range []wire.Message{
		{Type: wire.Ops},
		{Type: wire.Hello, Replica: "not valid!"},
	} {
		conn, _, err := websocket.Dial(t.Context(), base+"/ws?doc=pad", nil)
		require.NoError(t, err)
		require.NoError(t, wsjson.Write(t.Context(), conn, first))
		_, _, err = conn.Read(t.Context())
		require.Equal(t, websocket.StatusPolicyViolation, websocket.CloseStatus(err), fmt.Sprint(first))
	}
}

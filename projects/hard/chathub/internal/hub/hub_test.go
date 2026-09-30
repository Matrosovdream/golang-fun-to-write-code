package hub_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	"chathub/internal/hub"
)

// startHub runs a real server; tests talk to it over real WebSockets.
// Always run this package with -race: it is the whole point.
func startHub(t *testing.T) (*hub.Hub, string) {
	t.Helper()
	h := hub.New(slog.New(slog.NewTextHandler(io.Discard, nil)))
	go h.Run()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = h.Shutdown(ctx)
	})

	srv := httptest.NewServer(http.HandlerFunc(h.ServeWS))
	t.Cleanup(srv.Close)
	return h, "ws" + strings.TrimPrefix(srv.URL, "http")
}

type testClient struct {
	t    *testing.T
	conn *websocket.Conn
}

func dial(t *testing.T, wsURL, nick, room string) *testClient {
	t.Helper()
	conn, _, err := websocket.DefaultDialer.Dial(
		fmt.Sprintf("%s?nick=%s&room=%s", wsURL, nick, room), nil)
	require.NoError(t, err)
	t.Cleanup(func() { conn.Close() })
	return &testClient{t: t, conn: conn}
}

func (c *testClient) send(env hub.Envelope) {
	require.NoError(c.t, c.conn.WriteJSON(env))
}

// expect reads until an envelope of the wanted type arrives (skipping
// info/history noise) or times out.
func (c *testClient) expect(typ string) hub.Envelope {
	c.t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		require.NoError(c.t, c.conn.SetReadDeadline(deadline))
		var env hub.Envelope
		require.NoError(c.t, c.conn.ReadJSON(&env), "waiting for %q", typ)
		if env.Type == typ {
			return env
		}
	}
}

func TestMessageReachesRoomMates(t *testing.T) {
	_, wsURL := startHub(t)

	alice := dial(t, wsURL, "alice", "go")
	bob := dial(t, wsURL, "bob", "go")
	eve := dial(t, wsURL, "eve", "other") // different room, must hear nothing

	// Wait for join-info so both are registered before sending.
	alice.expect("history")
	bob.expect("history")
	eve.expect("history")

	alice.send(hub.Envelope{Type: "msg", Text: "hello bob"})

	got := bob.expect("msg")
	require.Equal(t, "alice", got.From)
	require.Equal(t, "hello bob", got.Text)
	require.Equal(t, "go", got.Room)

	// eve must not receive the message — only silence until the deadline.
	require.NoError(t, eve.conn.SetReadDeadline(time.Now().Add(300*time.Millisecond)))
	var env hub.Envelope
	for {
		if err := eve.conn.ReadJSON(&env); err != nil {
			break // timeout: correct
		}
		require.NotEqual(t, "msg", env.Type, "eve overheard another room")
	}
}

func TestHistoryReplayOnJoin(t *testing.T) {
	_, wsURL := startHub(t)

	alice := dial(t, wsURL, "alice", "go")
	alice.expect("history")
	alice.send(hub.Envelope{Type: "msg", Text: "first"})
	alice.send(hub.Envelope{Type: "msg", Text: "second"})
	alice.expect("msg")
	alice.expect("msg")

	late := dial(t, wsURL, "late", "go")
	history := late.expect("history")
	require.Len(t, history.Messages, 2)
	require.Equal(t, "first", history.Messages[0].Text)
	require.Equal(t, "second", history.Messages[1].Text)
}

func TestPresenceAndStats(t *testing.T) {
	h, wsURL := startHub(t)

	alice := dial(t, wsURL, "alice", "go")
	alice.expect("history")

	bob := dial(t, wsURL, "bob", "go")
	bob.expect("history")

	joined := alice.expect("info")
	require.Contains(t, joined.Text, "bob joined")

	stats := h.Stats()
	require.Equal(t, []hub.RoomStat{{Name: "go", Clients: 2}}, stats)

	bob.conn.Close()
	left := alice.expect("info")
	require.Contains(t, left.Text, "bob disconnected")
}

// The stress test: many clients sending concurrently, every client must
// receive every message. Readers run WHILE senders send — otherwise the
// hub (correctly) drops the non-reading clients as slow consumers.
// Meaningless without -race.
func TestConcurrentBroadcastDeliversEverything(t *testing.T) {
	_, wsURL := startHub(t)

	const clients, msgsEach = 8, 10
	const want = clients * msgsEach

	conns := make([]*testClient, clients)
	for i := range conns {
		conns[i] = dial(t, wsURL, fmt.Sprintf("user%d", i), "load")
		conns[i].expect("history")
	}
	// Let all join-infos settle so counting is exact.
	time.Sleep(200 * time.Millisecond)

	// require/t.FailNow must not run outside the test goroutine, so worker
	// goroutines report over a channel instead.
	errCh := make(chan error, 2*clients)
	var wg sync.WaitGroup

	for _, c := range conns {
		wg.Add(2)
		go func() { // reader
			defer wg.Done()
			got := 0
			_ = c.conn.SetReadDeadline(time.Now().Add(10 * time.Second))
			for got < want {
				var env hub.Envelope
				if err := c.conn.ReadJSON(&env); err != nil {
					errCh <- fmt.Errorf("read after %d msgs: %w", got, err)
					return
				}
				if env.Type == "msg" {
					if !strings.HasPrefix(env.Text, "m-") {
						errCh <- fmt.Errorf("unexpected text %q", env.Text)
						return
					}
					got++
				}
			}
		}()
		go func() { // sender
			defer wg.Done()
			for m := range msgsEach {
				env := hub.Envelope{Type: "msg", Text: fmt.Sprintf("m-%s-%d", c.conn.LocalAddr(), m)}
				if err := c.conn.WriteJSON(env); err != nil {
					errCh <- fmt.Errorf("send: %w", err)
					return
				}
			}
		}()
	}

	wg.Wait()
	close(errCh)
	for err := range errCh {
		require.NoError(t, err)
	}
}

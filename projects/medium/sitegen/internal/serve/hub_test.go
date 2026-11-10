package serve

import (
	"bufio"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// stream is an open SSE connection.
type stream struct {
	resp *http.Response
	r    *bufio.Reader
}

// connect opens the SSE endpoint and waits for the ": connected" preamble,
// which the handler writes after subscribing: events published from here on
// are guaranteed to reach this client.
func connect(t *testing.T, url string) *stream {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	t.Cleanup(cancel)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { resp.Body.Close() })

	s := &stream{resp: resp, r: bufio.NewReader(resp.Body)}
	line, err := s.r.ReadString('\n')
	require.NoError(t, err)
	require.Equal(t, ": connected\n", line)
	return s
}

func (s *stream) next(t *testing.T) (name, data string) {
	t.Helper()
	var lines []string
	for {
		line, err := s.r.ReadString('\n')
		require.NoError(t, err)
		line = strings.TrimSuffix(line, "\n")
		switch {
		case line == "" && name != "":
			return name, strings.Join(lines, "\n")
		case strings.HasPrefix(line, "event: "):
			name = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			lines = append(lines, strings.TrimPrefix(line, "data: "))
		}
	}
}

func TestHubStreamsEvents(t *testing.T) {
	hub := NewHub()
	srv := httptest.NewServer(hub)
	t.Cleanup(srv.Close) // registered before connect(): cleanups run last-in first-out
	s := connect(t, srv.URL)
	require.Equal(t, "text/event-stream", s.resp.Header.Get("Content-Type"))

	hub.Reload()
	name, data := s.next(t)
	require.Equal(t, "reload", name)
	require.Empty(t, data)

	hub.Fail(errors.New("content/a.md: bad\nsecond line"))
	name, data = s.next(t)
	require.Equal(t, "build-error", name)
	require.Equal(t, "content/a.md: bad\nsecond line", data, "multi-line payloads survive SSE framing")
}

func TestHubReplaysTheCurrentErrorToNewClientsOnly(t *testing.T) {
	hub := NewHub()
	srv := httptest.NewServer(hub)
	t.Cleanup(srv.Close) // registered before connect(): cleanups run last-in first-out

	hub.Fail(errors.New("broken"))
	name, data := connect(t, srv.URL).next(t)
	require.Equal(t, []string{"build-error", "broken"}, []string{name, data})

	hub.Reload() // a good build clears it
	late := connect(t, srv.URL)
	hub.Fail(errors.New("marker"))
	_, data = late.next(t)
	require.Equal(t, "marker", data, "no stale replay after a successful build")
}

func TestHubStopsWhenTheClientGoesAway(t *testing.T) {
	hub := NewHub()
	done := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(done)
		hub.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close) // registered before connect(): cleanups run last-in first-out

	ctx, cancel := context.WithCancel(t.Context())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	_, err = bufio.NewReader(resp.Body).ReadString('\n') // handler is subscribed and streaming
	require.NoError(t, err)

	cancel()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("handler still running after the client disconnected")
	}
	hub.mu.Lock()
	defer hub.mu.Unlock()
	require.Empty(t, hub.subs, "subscription leaked")
}

func TestHubNeverBlocksOnASlowClient(t *testing.T) {
	hub := NewHub()
	events, unsubscribe := hub.subscribe()
	defer unsubscribe()
	for range 100 { // nobody is reading
		hub.Reload()
	}
	require.LessOrEqual(t, len(events), cap(events))
}

func TestHubNeedsAFlusher(t *testing.T) {
	rec := httptest.NewRecorder()
	// Embedding only the interface hides the recorder's Flush method.
	NewHub().ServeHTTP(struct{ http.ResponseWriter }{rec}, httptest.NewRequest("GET", "/__reload", nil))
	require.Equal(t, http.StatusInternalServerError, rec.Code)
}

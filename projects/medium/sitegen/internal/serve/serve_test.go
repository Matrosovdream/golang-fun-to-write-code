package serve

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/require"

	"sitegen/internal/site"
)

func page(title, body string) *fstest.MapFile {
	return &fstest.MapFile{Data: []byte("---\ntitle: " + title + "\ndate: 2026-01-01\n---\n" + body + "\n")}
}

func newSite() fstest.MapFS {
	return fstest.MapFS{"content/posts/alpha.md": page("Alpha", "first body")}
}

func get(t *testing.T, url string) (int, string, http.Header) {
	t.Helper()
	resp, err := http.Get(url)
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, string(body), resp.Header
}

func TestInject(t *testing.T) {
	tests := []struct{ name, in string }{
		{"before the closing body tag", "<html><body><p>x</p></body></html>"},
		{"last closing tag wins", "<body><pre>&lt;/body&gt; </body> text</body></html>"},
		{"no body tag: appended", "<p>fragment</p>"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := []byte(tt.in)
			got := string(inject(in))
			require.Equal(t, tt.in, string(in), "input must not be modified")
			require.Equal(t, 1, strings.Count(got, reloadScript))
			if i := strings.LastIndex(tt.in, "</body>"); i >= 0 {
				require.Equal(t, tt.in[:i]+reloadScript+tt.in[i:], got)
			} else {
				require.Equal(t, tt.in+reloadScript, got)
			}
		})
	}
}

func TestServesTheBuildWithTheReloadScriptInjected(t *testing.T) {
	s := New(newSite(), site.New(), io.Discard)
	require.NoError(t, s.Rebuild(t.Context()))
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close) // registered before connect(): cleanups run last-in first-out

	code, body, hdr := get(t, srv.URL+"/")
	require.Equal(t, 200, code)
	require.Contains(t, hdr.Get("Content-Type"), "text/html")
	require.Contains(t, body, `new EventSource("/__reload")`)
	require.Less(t, strings.Index(body, "EventSource"), strings.Index(body, "</body>"))
	require.Contains(t, body, "Alpha")

	_, body, _ = get(t, srv.URL+"/posts/alpha") // no trailing slash: redirected, then served
	require.Contains(t, body, "first body")

	code, body, hdr = get(t, srv.URL+"/style.css")
	require.Equal(t, 200, code)
	require.Contains(t, hdr.Get("Content-Type"), "text/css")
	require.NotContains(t, body, "EventSource")

	_, body, _ = get(t, srv.URL+"/feed.xml")
	require.NotContains(t, body, "EventSource")

	code, _, _ = get(t, srv.URL+"/nope/")
	require.Equal(t, 404, code)

	stored, _ := s.out.Get("index.html")
	require.NotContains(t, string(stored), "__reload", "injection happens per response, never in the build output")
}

func TestFailedRebuildKeepsServingAndShowsTheError(t *testing.T) {
	src := newSite()
	s := New(src, site.New(), io.Discard)
	require.NoError(t, s.Rebuild(t.Context()))
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close) // registered before connect(): cleanups run last-in first-out
	events := connect(t, srv.URL+"/__reload")

	src["content/posts/alpha.md"] = &fstest.MapFile{Data: []byte("---\ntitle: [oops\n---\n")}
	err := s.Rebuild(t.Context())
	require.ErrorContains(t, err, "content/posts/alpha.md")
	name, data := events.next(t)
	require.Equal(t, "build-error", name)
	require.Contains(t, data, "content/posts/alpha.md")
	_, body, _ := get(t, srv.URL+"/posts/alpha/")
	require.Contains(t, body, "first body", "the last good build is still served")

	src["content/posts/alpha.md"] = page("Alpha", "fixed body")
	require.NoError(t, s.Rebuild(t.Context()))
	name, _ = events.next(t)
	require.Equal(t, "reload", name)
	_, body, _ = get(t, srv.URL+"/posts/alpha/")
	require.Contains(t, body, "fixed body")
}

func TestRunRebuildsOnChangeAndStopsCleanly(t *testing.T) {
	src := newSite()
	var log bytes.Buffer
	s := New(src, site.New(), &log)
	require.NoError(t, s.Rebuild(t.Context()))

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	changes := make(chan struct{})
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx, ln, changes) }()
	base := "http://" + ln.Addr().String()
	events := connect(t, base+"/__reload")

	src["content/posts/alpha.md"] = page("Alpha", "edited body")
	changes <- struct{}{}
	name, _ := events.next(t)
	require.Equal(t, "reload", name)
	_, body, _ := get(t, base+"/posts/alpha/")
	require.Contains(t, body, "edited body")

	src["content/posts/alpha.md"] = &fstest.MapFile{Data: []byte("---\nnope: 1\n---\n")}
	changes <- struct{}{}
	name, data := events.next(t)
	require.Equal(t, "build-error", name)
	require.Contains(t, data, "nope")

	cancel() // the SSE client is still connected: Shutdown must not wait for it
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return")
	}
	require.Contains(t, log.String(), "build failed")
}

func TestRunStopsWhenChangesCloses(t *testing.T) {
	s := New(newSite(), site.New(), io.Discard)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	changes := make(chan struct{})
	done := make(chan error, 1)
	go func() { done <- s.Run(t.Context(), ln, changes) }()
	connect(t, "http://"+ln.Addr().String()+"/__reload")

	close(changes)
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return")
	}
}

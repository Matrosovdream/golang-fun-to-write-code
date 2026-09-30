package crawler_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"fetchpool/internal/crawler"
)

// testSite serves a tiny interlinked site:
//
//	/ → /a, /b        /a → /b, /deep      /b → (no links)
//	/deep → /deeper    /missing → 404      /slow → sleeps
func testSite(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	page := func(links ...string) http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			for _, l := range links {
				fmt.Fprintf(w, `<a href=%q>x</a>`, l)
			}
		}
	}
	mux.HandleFunc("/", page("/a", "/b", "/missing"))
	mux.HandleFunc("/a", page("/b", "/deep"))
	mux.HandleFunc("/b", page())
	mux.HandleFunc("/deep", page("/deeper"))
	mux.HandleFunc("/deeper", page())
	mux.HandleFunc("/missing", http.NotFound)
	mux.HandleFunc("/slow", func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(5 * time.Second):
		case <-r.Context().Done():
		}
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func urlsOf(results []crawler.Result) map[string]crawler.Result {
	m := make(map[string]crawler.Result, len(results))
	for _, r := range results {
		m[r.URL] = r
	}
	return m
}

func TestCrawlFindsWholeSiteWithinDepth(t *testing.T) {
	srv := testSite(t)
	c := crawler.New(crawler.WithWorkers(4), crawler.WithRate(1000), crawler.WithMaxDepth(3))

	// mux serves "/" for unknown paths too, so /missing needs its own route
	// above to actually 404.
	results, err := c.Crawl(context.Background(), srv.URL+"/")
	require.NoError(t, err)

	got := urlsOf(results)
	for _, path := range []string{"/", "/a", "/b", "/deep", "/deeper", "/missing"} {
		require.Contains(t, got, srv.URL+path)
	}
	require.Equal(t, 404, got[srv.URL+"/missing"].Status)
}

func TestCrawlRespectsMaxDepth(t *testing.T) {
	srv := testSite(t)
	c := crawler.New(crawler.WithWorkers(4), crawler.WithRate(1000), crawler.WithMaxDepth(1))

	// depth 0 is "/", depth 1 its links — /deep's links must not be fetched.
	results, err := c.Crawl(context.Background(), srv.URL+"/")
	require.NoError(t, err)

	got := urlsOf(results)
	require.Contains(t, got, srv.URL+"/a")
	require.NotContains(t, got, srv.URL+"/deep") // linked from /a at depth 2
}

func TestCheckAllCountsOutcomes(t *testing.T) {
	srv := testSite(t)
	c := crawler.New(crawler.WithWorkers(4), crawler.WithRate(1000))

	urls := []string{
		srv.URL + "/",
		srv.URL + "/b",
		srv.URL + "/missing",
		"http://127.0.0.1:1/unreachable",
	}
	results, stats, err := c.CheckAll(context.Background(), urls)
	require.NoError(t, err)
	require.Len(t, results, len(urls))
	require.EqualValues(t, 2, stats.OK.Load())
	require.EqualValues(t, 1, stats.Broken.Load())
	require.EqualValues(t, 1, stats.Failed.Load())
}

func TestCrawlCancellationReturnsPromptly(t *testing.T) {
	srv := testSite(t)
	c := crawler.New(crawler.WithWorkers(2), crawler.WithRate(1000), crawler.WithMaxDepth(5))

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, err := c.Crawl(ctx, srv.URL+"/slow")
		require.ErrorIs(t, err, context.DeadlineExceeded)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("crawl did not stop after context cancellation")
	}
}

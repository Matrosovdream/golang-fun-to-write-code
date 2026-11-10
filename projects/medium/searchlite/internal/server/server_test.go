package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"searchlite/internal/corpus"
	"searchlite/internal/index"
)

type searchResp struct {
	Total  int            `json:"total"`
	Hits   []index.Result `json:"hits"`
	Error  string         `json:"error"`
	Offset *int           `json:"offset"`
}

func start(t *testing.T, ix *index.Index) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(Handler(ix))
	t.Cleanup(ts.Close)
	return ts
}

func demo(t *testing.T) *httptest.Server {
	t.Helper()
	ix := index.New()
	require.NoError(t, ix.AddFS(corpus.FS()))
	return start(t, ix)
}

func get(t testing.TB, ts *httptest.Server, params url.Values) (int, searchResp) {
	t.Helper()
	resp, err := http.Get(ts.URL + "/search?" + params.Encode())
	require.NoError(t, err)
	defer resp.Body.Close()
	var out searchResp
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	return resp.StatusCode, out
}

func post(t testing.TB, ts *httptest.Server, body string) int {
	t.Helper()
	resp, err := http.Post(ts.URL+"/docs", "application/json", strings.NewReader(body))
	require.NoError(t, err)
	resp.Body.Close()
	return resp.StatusCode
}

func TestSearch(t *testing.T) {
	ts := demo(t)
	code, out := get(t, ts, url.Values{"q": {`goroutine AND "memory model" -mutex`}})
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, 1, out.Total)
	require.Equal(t, "race-detector.md", out.Hits[0].Name)
	require.Contains(t, out.Hits[0].Snippet, "[[goroutines]]")

	_, out = get(t, ts, url.Values{"q": {"channel"}, "k": {"2"}})
	require.Greater(t, out.Total, 2)
	require.Len(t, out.Hits, 2)

	_, out = get(t, ts, url.Values{"q": {"zzzzzz"}})
	require.NotNil(t, out.Hits, "no matches is [], not null")
}

func TestSearchParseError(t *testing.T) {
	ts := demo(t)
	code, out := get(t, ts, url.Values{"q": {"goroutine AND )"}})
	require.Equal(t, http.StatusBadRequest, code)
	require.Contains(t, out.Error, "offset 14")
	require.Equal(t, 14, *out.Offset)

	code, _ = get(t, ts, url.Values{})
	require.Equal(t, http.StatusBadRequest, code)
}

func TestAddDoc(t *testing.T) {
	ts := start(t, index.New())
	require.Equal(t, http.StatusCreated, post(t, ts, `{"name":"a.md","text":"# Zebras\nstriped animals"}`))
	_, out := get(t, ts, url.Values{"q": {"zebra"}})
	require.Equal(t, "Zebras", out.Hits[0].Title)

	require.Equal(t, http.StatusConflict, post(t, ts, `{"name":"a.md","text":"again"}`))
	for _, body := range []string{``, `{`, `{"name":"b.md"}`, `{"name":"","text":"x"}`, `{"name":"b.md","text":"  "}`} {
		require.Equal(t, http.StatusBadRequest, post(t, ts, body), body)
	}
	require.Equal(t, http.StatusBadRequest, post(t, ts, `{"name":"big.md","text":"`+strings.Repeat("x", maxDocBytes)+`"}`))
}

// POST /docs while searches run, through the real handlers, under -race.
func TestIndexWhileSearching(t *testing.T) {
	ts := demo(t)
	const writers, perWriter = 3, 10
	var stop atomic.Bool
	var wg, readers sync.WaitGroup
	for w := range writers {
		wg.Go(func() {
			for i := range perWriter {
				body := fmt.Sprintf(`{"name":"w%d-%d.md","text":"quokka %d"}`, w, i, i)
				assert.Equal(t, http.StatusCreated, post(t, ts, body))
			}
		})
	}
	for range 3 {
		readers.Go(func() {
			seen := 0
			for !stop.Load() {
				code, out := get(t, ts, url.Values{"q": {"quokka OR goroutine"}})
				assert.Equal(t, http.StatusOK, code)
				assert.GreaterOrEqual(t, out.Total, seen, "documents only ever get added")
				seen = out.Total
			}
		})
	}
	wg.Wait()
	stop.Store(true)
	readers.Wait()

	_, out := get(t, ts, url.Values{"q": {"quokka"}})
	require.Equal(t, writers*perWriter, out.Total)
}

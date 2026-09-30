package handler_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"shortlink/internal/handler"
	"shortlink/internal/repo"
	"shortlink/internal/service"
)

// newServer assembles the real stack on the in-memory repo — these are
// black-box tests through actual HTTP.
func newServer(t *testing.T) *httptest.Server {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := handler.New(service.New(repo.NewMemory()), "http://sho.rt", log)
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv
}

func TestShortenResolveStats(t *testing.T) {
	srv := newServer(t)

	res, err := http.Post(srv.URL+"/api/links", "application/json",
		strings.NewReader(`{"url": "https://go.dev/doc/"}`))
	require.NoError(t, err)
	defer res.Body.Close()
	require.Equal(t, http.StatusCreated, res.StatusCode)

	var created struct {
		Code     string `json:"code"`
		ShortURL string `json:"short_url"`
	}
	require.NoError(t, json.NewDecoder(res.Body).Decode(&created))
	require.Len(t, created.Code, 7)
	require.Equal(t, "http://sho.rt/"+created.Code, created.ShortURL)

	// Disable redirect-following so we can inspect the 302 itself.
	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	for range 3 {
		res, err := client.Get(srv.URL + "/" + created.Code)
		require.NoError(t, err)
		res.Body.Close()
		require.Equal(t, http.StatusFound, res.StatusCode)
		require.Equal(t, "https://go.dev/doc/", res.Header.Get("Location"))
	}

	res, err = http.Get(srv.URL + "/api/links/" + created.Code)
	require.NoError(t, err)
	defer res.Body.Close()

	var stats struct {
		Hits int64 `json:"hits"`
	}
	require.NoError(t, json.NewDecoder(res.Body).Decode(&stats))
	require.EqualValues(t, 3, stats.Hits)
}

func TestShortenRejectsBadInput(t *testing.T) {
	srv := newServer(t)

	tests := []struct {
		name string
		body string
	}{
		{"not json", `hello`},
		{"missing url", `{}`},
		{"relative url", `{"url": "/just/a/path"}`},
		{"wrong scheme", `{"url": "ftp://example.com/f"}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res, err := http.Post(srv.URL+"/api/links", "application/json",
				strings.NewReader(tc.body))
			require.NoError(t, err)
			res.Body.Close()
			require.Equal(t, http.StatusBadRequest, res.StatusCode)
		})
	}
}

func TestUnknownCodeIs404(t *testing.T) {
	srv := newServer(t)

	res, err := http.Get(srv.URL + "/api/links/nope123")
	require.NoError(t, err)
	res.Body.Close()
	require.Equal(t, http.StatusNotFound, res.StatusCode)
}

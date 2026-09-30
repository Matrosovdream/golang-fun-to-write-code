package httpx_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"weathercache/internal/httpx"
)

func flakyServer(t *testing.T, failures int32) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) <= failures {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func newClient() *http.Client {
	return &http.Client{
		Transport: &httpx.RetryTransport{MaxAttempts: 3, BaseDelay: time.Millisecond},
	}
}

func TestRetriesUntilSuccess(t *testing.T) {
	srv, calls := flakyServer(t, 2)

	res, err := newClient().Get(srv.URL)
	require.NoError(t, err)
	res.Body.Close()
	require.Equal(t, http.StatusOK, res.StatusCode)
	require.EqualValues(t, 3, calls.Load())
}

func TestGivesUpAfterMaxAttempts(t *testing.T) {
	srv, calls := flakyServer(t, 100)

	_, err := newClient().Get(srv.URL) //nolint:bodyclose // errors carry no body
	require.Error(t, err)
	require.Contains(t, err.Error(), "giving up after 3 attempts")
	require.EqualValues(t, 3, calls.Load())
}

func TestPostIsNotRetried(t *testing.T) {
	srv, calls := flakyServer(t, 100)

	res, err := newClient().Post(srv.URL, "text/plain", strings.NewReader("data"))
	require.NoError(t, err) // a 500 is a valid response, not a transport error
	res.Body.Close()
	require.Equal(t, http.StatusInternalServerError, res.StatusCode)
	require.EqualValues(t, 1, calls.Load())
}

package server_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"raftkv/internal/clock"
	"raftkv/internal/kv"
	"raftkv/internal/raft"
	"raftkv/internal/server"
	"raftkv/internal/transport"
)

// startCluster runs three real nodes in this process, talking HTTP over
// loopback on the real clock: the same wiring as cmd/raftkv.
func startCluster(t *testing.T) map[raft.NodeID]string {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	ids := []raft.NodeID{1, 2, 3}
	lis := map[raft.NodeID]net.Listener{}
	addrs := map[raft.NodeID]string{}
	for _, id := range ids {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)
		lis[id], addrs[id] = l, l.Addr().String()
	}
	for _, id := range ids {
		node, err := raft.New(raft.Config{
			ID: id, Peers: ids, Storage: &raft.MemStorage{}, Machine: kv.NewStore(),
			Transport: transport.NewHTTP(ctx, id, addrs), Clock: clock.Real{}, Seed: uint64(id),
		})
		require.NoError(t, err)
		go node.Run(ctx)

		mux := http.NewServeMux()
		mux.Handle("POST /raft", transport.Handler(node))
		mux.Handle("/", server.New(node, addrs).Handler())
		srv := &http.Server{Handler: mux}
		go srv.Serve(lis[id])
		t.Cleanup(func() { srv.Close() })
	}
	return addrs
}

func do(t *testing.T, c *http.Client, method, url, body string, hdr ...string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	require.NoError(t, err)
	for i := 0; i < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	resp, err := c.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, string(b)
}

func TestAPIThroughAnyNode(t *testing.T) {
	addrs := startCluster(t)
	url := func(id raft.NodeID, path string) string { return fmt.Sprintf("http://%s%s", addrs[id], path) }
	follow := &http.Client{Timeout: 10 * time.Second}
	noFollow := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

	// Wait until every node agrees on who leads, so redirects have a target.
	var leader raft.NodeID
	require.Eventually(t, func() bool {
		leader = 0
		for id := raft.NodeID(1); id <= 3; id++ {
			resp, err := follow.Get(url(id, "/status"))
			if err != nil {
				return false
			}
			var st struct{ Leader raft.NodeID }
			err = json.NewDecoder(resp.Body).Decode(&st)
			resp.Body.Close()
			if err != nil || st.Leader == 0 || (leader != 0 && st.Leader != leader) {
				return false
			}
			leader = st.Leader
		}
		return true
	}, 30*time.Second, 50*time.Millisecond)
	follower := leader%3 + 1

	code, _ := do(t, noFollow, "PUT", url(follower, "/kv/color"), "blue")
	require.Equal(t, http.StatusTemporaryRedirect, code, "a follower redirects")
	resp, err := noFollow.Get(url(follower, "/kv/color"))
	require.NoError(t, err)
	resp.Body.Close()
	require.Equal(t, "http://"+addrs[leader]+"/kv/color", resp.Header.Get("Location"))

	code, _ = do(t, follow, "PUT", url(follower, "/kv/color"), "blue") // the client follows the 307, body included
	require.Equal(t, http.StatusNoContent, code)
	for id := raft.NodeID(1); id <= 3; id++ {
		code, body := do(t, follow, "GET", url(id, "/kv/color"), "")
		require.Equal(t, http.StatusOK, code)
		require.Equal(t, "blue", body)
	}

	code, _ = do(t, follow, "DELETE", url(follower, "/kv/color"), "")
	require.Equal(t, http.StatusNoContent, code)
	code, _ = do(t, follow, "GET", url(follower, "/kv/color"), "")
	require.Equal(t, http.StatusNotFound, code)

	// A retried request with the same X-Client-ID/X-Seq is applied once.
	hdr := []string{"X-Client-ID", "curl-1", "X-Seq", "1"}
	do(t, follow, "PUT", url(leader, "/kv/n"), "first", hdr...)
	do(t, follow, "PUT", url(leader, "/kv/n"), "other-client")
	do(t, follow, "PUT", url(leader, "/kv/n"), "first", hdr...) // the retry
	_, body := do(t, follow, "GET", url(leader, "/kv/n"), "")
	require.Equal(t, "other-client", body)

	code, _ = do(t, follow, "PUT", url(leader, "/kv/n"), "x", "X-Seq", "nope")
	require.Equal(t, http.StatusBadRequest, code)
}

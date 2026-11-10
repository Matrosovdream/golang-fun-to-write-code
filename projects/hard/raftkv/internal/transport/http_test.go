package transport_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"raftkv/internal/raft"
	"raftkv/internal/transport"
)

func TestHTTPCarriesMessages(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	recv := &inbox{}
	mux := http.NewServeMux()
	mux.Handle("POST /raft", transport.Handler(recv))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	tr := transport.NewHTTP(ctx, 1, map[raft.NodeID]string{
		1: "127.0.0.1:1",
		2: srv.Listener.Addr().String(),
		3: "127.0.0.1:1", // nobody listens: must cost nothing but dropped messages
	})
	want := raft.Message{
		Type: raft.MsgAppendEntries, From: 1, To: 2, Term: 7,
		PrevLogIndex: 3, PrevLogTerm: 2, LeaderCommit: 3,
		Entries: []raft.Entry{{Term: 7, Data: []byte(`{"op":"put"}`)}, {Term: 7}},
	}
	for range 200 { // far more than the queue holds
		tr.Send(raft.Message{From: 1, To: 3})
	}
	tr.Send(want)
	tr.Send(raft.Message{From: 1, To: 99}) // unknown peer: dropped

	require.Eventually(t, func() bool { return recv.count() == 1 }, 10*time.Second, 10*time.Millisecond)
	require.Equal(t, want, recv.msgs[0])
}

func TestHandlerRejectsGarbage(t *testing.T) {
	rec := httptest.NewRecorder()
	transport.Handler(&inbox{}).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/raft", nil))
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

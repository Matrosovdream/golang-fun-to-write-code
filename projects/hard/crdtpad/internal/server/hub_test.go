package server

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"crdtpad/internal/rga"
	"crdtpad/internal/wire"
)

type testPeer struct {
	*peer
	kicked chan struct{}
}

func newPeer(replica string, buffer int) *testPeer {
	kicked := make(chan struct{})
	return &testPeer{
		peer:   &peer{replica: replica, send: make(chan wire.Message, buffer), kick: sync.OnceFunc(func() { close(kicked) })},
		kicked: kicked,
	}
}

func (p *testPeer) recv(t *testing.T) wire.Message {
	t.Helper()
	select {
	case m := <-p.send:
		return m
	case <-time.After(10 * time.Second):
		require.FailNow(t, "no message for "+p.replica)
		return wire.Message{}
	}
}

func startHub(t *testing.T) *docHub {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	s := New(ctx, t.TempDir(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(func() { cancel(); s.Wait() })
	h, err := s.hub("pad")
	require.NoError(t, err)
	return h
}

func typed(t *testing.T, replica, text string) []rga.Op {
	t.Helper()
	d := rga.New(replica)
	var ops []rga.Op
	for i, r := range text {
		op, err := d.Insert(i, r)
		require.NoError(t, err)
		ops = append(ops, op)
	}
	return ops
}

func join1(t *testing.T, h *docHub, p *testPeer, version rga.Version) wire.Message {
	t.Helper()
	h.joins <- join{p.peer, version}
	m := p.recv(t)
	require.Equal(t, wire.Sync, m.Type)
	return m
}

func TestHelloSyncSendsOnlyMissingOps(t *testing.T) {
	h := startHub(t)
	ops := typed(t, "alice", "abc")
	alice := newPeer("alice", 8)
	join1(t, h, alice, nil)
	h.in <- inbound{alice.peer, ops}
	require.Equal(t, ops, alice.recv(t).Ops)

	bob := newPeer("bob", 8)
	m := join1(t, h, bob, nil)
	require.Equal(t, ops, m.Ops)
	require.Equal(t, rga.Version{"alice": 3}, m.Version)

	carol := newPeer("carol", 8)
	m = join1(t, h, carol, rga.Version{"alice": 1})
	require.Equal(t, ops[1:], m.Ops)
}

func TestOutOfOrderOpsAreBroadcastWhenReleased(t *testing.T) {
	h := startHub(t)
	ops := typed(t, "alice", "ab")
	alice, bob := newPeer("alice", 8), newPeer("bob", 8)
	join1(t, h, alice, nil)
	join1(t, h, bob, nil)

	h.in <- inbound{alice.peer, ops[1:]} // buffered: its parent has not arrived
	h.in <- inbound{alice.peer, ops[:1]}
	require.Equal(t, ops, bob.recv(t).Ops, "the first thing bob hears is both ops, in causal order")
}

func TestSlowPeerIsDroppedWithoutBlockingOthers(t *testing.T) {
	h := startHub(t)
	slow, fast := newPeer("slow", 1), newPeer("fast", 8)
	join1(t, h, fast, nil)
	h.joins <- join{slow.peer, nil} // its one buffer slot now holds the sync, and nobody drains it

	for i, op := range typed(t, "fast", "xyz") {
		h.in <- inbound{fast.peer, []rga.Op{op}}
		require.Equal(t, []rga.Op{op}, fast.recv(t).Ops, "op %d", i)
	}
	select {
	case <-slow.kicked:
	default:
		require.FailNow(t, "slow peer was not kicked")
	}
	text, ok := h.text(t.Context())
	require.True(t, ok)
	require.Equal(t, "xyz", text)
}

func TestInvalidOpDropsOnlyTheSender(t *testing.T) {
	h := startHub(t)
	bad, good := newPeer("bad", 8), newPeer("good", 8)
	join1(t, h, bad, nil)
	join1(t, h, good, nil)

	h.in <- inbound{bad.peer, []rga.Op{{Replica: "bad", Seq: 1, Kind: "upd"}}}
	<-bad.kicked
	select {
	case <-good.kicked:
		require.FailNow(t, "innocent peer kicked")
	default:
	}
	text, _ := h.text(t.Context())
	require.Empty(t, text)
}

func TestOplogReplaysAndRejectsCorruption(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pad.oplog")
	ops := typed(t, "alice", "hi")
	l, err := openOplog(path, func(rga.Op) error { return nil })
	require.NoError(t, err)
	for _, op := range ops {
		require.NoError(t, l.Append(op))
	}
	require.NoError(t, l.Close())

	var got []rga.Op
	l, err = openOplog(path, func(op rga.Op) error { got = append(got, op); return nil })
	require.NoError(t, err)
	require.Equal(t, ops, got)
	require.NoError(t, l.Close())

	h, err := openHub(path, slog.New(slog.NewTextHandler(io.Discard, nil)))
	require.NoError(t, err)
	require.Equal(t, "hi", h.doc.Text())
	h.store.Close()
}

func TestOpenHubRejectsBrokenLogs(t *testing.T) {
	write := func(content string) string {
		path := filepath.Join(t.TempDir(), "pad.oplog")
		l, err := openOplog(path, func(rga.Op) error { return nil })
		require.NoError(t, err)
		_, err = l.f.WriteString(content)
		require.NoError(t, err)
		require.NoError(t, l.Close())
		return path
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	_, err := openHub(write(`{"replica":"alice","seq":1,"ki`), log)
	require.ErrorContains(t, err, "op 1")

	// Only the second op survived: it depends on one that is not in the log.
	_, err = openHub(write(`{"replica":"alice","seq":2,"kind":"ins","clock":2,"after":{"clock":1,"replica":"alice"},"char":98}`+"\n"), log)
	require.ErrorContains(t, err, "not in the log")
}

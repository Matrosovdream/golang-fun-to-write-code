package server

import (
	"context"
	"fmt"
	"log/slog"

	"crdtpad/internal/rga"
	"crdtpad/internal/wire"
)

// sendBuffer is how far behind a peer's socket may fall before it is dropped.
const sendBuffer = 256

// peer is the hub's view of one connection. The hub only ever sends on `send`
// (never blocking) and calls `kick`; the connection's own goroutines do the I/O.
type peer struct {
	replica string
	send    chan wire.Message
	kick    func()
}

type join struct {
	p       *peer
	version rga.Version
}

type inbound struct {
	from *peer
	ops  []rga.Op
}

// docHub owns one document. Like chathub's Hub, everything mutable (the
// rga.Doc, the oplog, the peer set) belongs to the run goroutine, so there
// are no locks; connection goroutines talk to it over channels.
type docHub struct {
	log   *slog.Logger
	doc   *rga.Doc
	store *oplog
	peers map[*peer]struct{}

	joins  chan join
	leaves chan *peer
	in     chan inbound
	texts  chan chan string
	done   chan struct{} // closed when run returns
}

// openHub rebuilds the document from its oplog.
func openHub(path string, log *slog.Logger) (*docHub, error) {
	doc := rga.New("server")
	store, err := openOplog(path, func(op rga.Op) error {
		_, err := doc.Apply(op)
		return err
	})
	if err != nil {
		return nil, err
	}
	if n := doc.Pending(); n > 0 {
		store.Close()
		return nil, fmt.Errorf("oplog %s: %d ops depend on ops that are not in the log", path, n)
	}
	log.Info("document opened", "ops", len(doc.Missing(nil)))
	return &docHub{
		log:    log,
		doc:    doc,
		store:  store,
		peers:  map[*peer]struct{}{},
		joins:  make(chan join),
		leaves: make(chan *peer),
		in:     make(chan inbound),
		texts:  make(chan chan string),
		done:   make(chan struct{}),
	}, nil
}

func (h *docHub) run(ctx context.Context) {
	defer close(h.done)
	defer h.store.Close()
	defer func() {
		for p := range h.peers {
			p.kick()
		}
	}()
	for {
		select {
		case j := <-h.joins:
			h.peers[j.p] = struct{}{}
			h.trySend(j.p, wire.Message{Type: wire.Sync, Version: h.doc.Version(), Ops: h.doc.Missing(j.version)})
		case p := <-h.leaves:
			delete(h.peers, p)
		case in := <-h.in:
			if !h.handle(in) {
				return
			}
		case reply := <-h.texts:
			reply <- h.doc.Text()
		case <-ctx.Done():
			return
		}
	}
}

// handle applies a peer's ops, persists exactly what became applied, and
// broadcasts it to everyone (the sender included: a duplicate is a no-op
// there, and ops released from the pending buffer may be news to it).
// It returns false if the oplog can no longer be written; the hub then
// stops rather than keep serving a document it cannot make durable.
func (h *docHub) handle(in inbound) bool {
	var applied []rga.Op
	for _, op := range in.ops {
		got, err := h.doc.Apply(op)
		if err != nil {
			h.drop(in.from, "invalid op: "+err.Error())
			break
		}
		applied = append(applied, got...)
	}
	for _, op := range applied {
		if err := h.store.Append(op); err != nil {
			h.log.Error("oplog write failed, closing document", "err", err)
			return false
		}
	}
	if len(applied) > 0 {
		for p := range h.peers {
			h.trySend(p, wire.Message{Type: wire.Ops, Ops: applied})
		}
	}
	return true
}

// trySend never blocks the loop: one stalled socket must not stall the
// document. As in chathub the slow peer is dropped, not the message, so
// everyone else keeps an in-order stream; the dropped client reconnects and
// its hello version makes the server resend exactly what it missed.
func (h *docHub) trySend(p *peer, m wire.Message) {
	select {
	case p.send <- m:
	default:
		h.drop(p, "slow consumer")
	}
}

func (h *docHub) drop(p *peer, why string) {
	h.log.Warn("dropping peer", "replica", p.replica, "reason", why)
	delete(h.peers, p)
	p.kick()
}

// deliver hands v to the hub loop unless the caller gives up or the hub is gone.
func deliver[T any](ctx context.Context, h *docHub, ch chan<- T, v T) bool {
	select {
	case ch <- v:
		return true
	case <-ctx.Done():
		return false
	case <-h.done:
		return false
	}
}

// text is the request/response-over-a-channel idiom for reading loop-owned state.
func (h *docHub) text(ctx context.Context) (string, bool) {
	reply := make(chan string, 1)
	if !deliver(ctx, h, h.texts, reply) {
		return "", false
	}
	return <-reply, true
}

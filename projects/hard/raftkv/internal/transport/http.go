package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"time"

	"raftkv/internal/raft"
)

const queueSize = 64

// HTTP sends raft.Messages as JSON POSTs to peers' /raft endpoint. Each peer
// has a bounded queue and one goroutine draining it, so a dead peer costs a
// full queue (dropped messages, which Raft tolerates) instead of unbounded
// goroutines, and messages to one peer stay in order.
type HTTP struct {
	queues map[raft.NodeID]chan raft.Message
	client *http.Client
}

// NewHTTP starts a sender per peer in addrs (id -> host:port) other than self.
// The senders stop when ctx is cancelled.
func NewHTTP(ctx context.Context, self raft.NodeID, addrs map[raft.NodeID]string) *HTTP {
	h := &HTTP{
		queues: make(map[raft.NodeID]chan raft.Message),
		client: &http.Client{Timeout: time.Second},
	}
	for id, addr := range addrs {
		if id == self {
			continue
		}
		q := make(chan raft.Message, queueSize)
		h.queues[id] = q
		go h.pump(ctx, "http://"+addr+"/raft", q)
	}
	return h
}

func (h *HTTP) Send(m raft.Message) {
	select {
	case h.queues[m.To] <- m:
	default:
	}
}

func (h *HTTP) pump(ctx context.Context, url string, q <-chan raft.Message) {
	for {
		select {
		case <-ctx.Done():
			return
		case m := <-q:
			body, _ := json.Marshal(m) // Message holds only plain data; Marshal cannot fail
			if resp, err := h.client.Post(url, "application/json", bytes.NewReader(body)); err == nil {
				resp.Body.Close()
			}
		}
	}
}

// Handler is the receiving side: it decodes a message and hands it to r.
func Handler(r Receiver) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var m raft.Message
		if err := json.NewDecoder(http.MaxBytesReader(w, req.Body, 4<<20)).Decode(&m); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		r.Receive(m)
		w.WriteHeader(http.StatusNoContent)
	})
}

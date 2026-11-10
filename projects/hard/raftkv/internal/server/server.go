// Package server is the client-facing HTTP KV API. Every node serves it;
// followers redirect to the leader.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"raftkv/internal/kv"
	"raftkv/internal/raft"
)

// Node is the part of *raft.Node the API needs.
type Node interface {
	kv.Proposer
	Status() raft.Status
}

type Server struct {
	node    Node
	addrs   map[raft.NodeID]string // node id -> host:port of its HTTP server
	timeout time.Duration
}

func New(node Node, addrs map[raft.NodeID]string) *Server {
	return &Server{node: node, addrs: addrs, timeout: 3 * time.Second}
}

// Handler serves:
//
//	PUT    /kv/{key}   body = value                  -> 204
//	GET    /kv/{key}                                 -> 200 value | 404
//	DELETE /kv/{key}                                 -> 204
//	GET    /status                                   -> this node's Raft state
//
// A request may carry X-Client-ID and X-Seq. A retry with the same pair is
// never applied twice, so after a 504 or a dropped connection resend it
// unchanged. Without them a retry can apply twice.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /kv/{key}", s.command(kv.Put))
	mux.HandleFunc("GET /kv/{key}", s.command(kv.Get))
	mux.HandleFunc("DELETE /kv/{key}", s.command(kv.Delete))
	mux.HandleFunc("GET /status", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(s.node.Status())
	})
	return mux
}

func (s *Server) command(op kv.Op) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cmd := kv.Command{Op: op, Key: r.PathValue("key"), ClientID: r.Header.Get("X-Client-ID")}
		if v := r.Header.Get("X-Seq"); v != "" {
			seq, err := strconv.ParseUint(v, 10, 64)
			if err != nil {
				http.Error(w, "bad X-Seq", http.StatusBadRequest)
				return
			}
			cmd.Seq = seq
		}
		if op == kv.Put {
			body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
			if err != nil {
				http.Error(w, err.Error(), http.StatusRequestEntityTooLarge)
				return
			}
			cmd.Value = string(body)
		}

		ctx, cancel := context.WithTimeout(r.Context(), s.timeout)
		defer cancel()
		res, err := kv.Exec(ctx, s.node, cmd)
		switch {
		case errors.Is(err, raft.ErrNotLeader), errors.Is(err, raft.ErrStopped):
			s.redirect(w, r)
		case errors.Is(err, context.DeadlineExceeded):
			http.Error(w, "timed out; the command may still commit, retry it unchanged", http.StatusGatewayTimeout)
		case err != nil:
			http.Error(w, err.Error(), http.StatusInternalServerError)
		case op == kv.Get && !res.Found:
			http.Error(w, "not found", http.StatusNotFound)
		case op == kv.Get:
			io.WriteString(w, res.Value)
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}
}

// redirect sends the client to the leader. 307 keeps the method and body.
func (s *Server) redirect(w http.ResponseWriter, r *http.Request) {
	st := s.node.Status()
	addr, ok := s.addrs[st.Leader]
	if !ok || st.Leader == st.ID {
		w.Header().Set("Retry-After", "1")
		http.Error(w, "no leader known, retry shortly", http.StatusServiceUnavailable)
		return
	}
	http.Redirect(w, r, "http://"+addr+r.URL.RequestURI(), http.StatusTemporaryRedirect)
}

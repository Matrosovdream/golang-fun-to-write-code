// Package server serves collaborative documents over WebSockets: one hub
// goroutine per document, an append-only oplog per document, and the
// embedded browser client.
package server

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"regexp"
	"sync"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"crdtpad/internal/rga"
	"crdtpad/internal/wire"
	"crdtpad/web"
)

// maxMessage bounds what a client may send in one message (a batch of ops).
const maxMessage = 1 << 20

var docName = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

type Server struct {
	ctx  context.Context // lifetime of the hub goroutines
	dir  string
	log  *slog.Logger
	wg   sync.WaitGroup
	mu   sync.Mutex // guards hubs only; documents themselves are lock-free
	hubs map[string]*docHub
}

// New creates a server whose documents live in dir. Cancel ctx to stop the
// hubs, then call Wait for the oplogs to be closed.
func New(ctx context.Context, dir string, log *slog.Logger) *Server {
	return &Server{ctx: ctx, dir: dir, log: log, hubs: map[string]*docHub{}}
}

func (s *Server) Wait() { s.wg.Wait() }

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws", s.serveWS)
	mux.HandleFunc("GET /doc/{name}", s.serveDoc)
	mux.Handle("GET /", http.FileServerFS(web.Files))
	return mux
}

// hub returns the document's running hub, starting (and replaying) it on first use.
func (s *Server) hub(name string) (*docHub, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if h, ok := s.hubs[name]; ok {
		select {
		case <-h.done: // the hub stopped (oplog failure): reopen from disk
		default:
			return h, nil
		}
	}
	if err := s.ctx.Err(); err != nil {
		return nil, err
	}
	h, err := openHub(filepath.Join(s.dir, name+".oplog"), s.log.With("doc", name))
	if err != nil {
		return nil, err
	}
	s.hubs[name] = h
	s.wg.Go(func() { h.run(s.ctx) })
	return h, nil
}

// hubFor resolves ?doc= style names, writing the HTTP error itself on failure.
func (s *Server) hubFor(w http.ResponseWriter, name string) *docHub {
	if !docName.MatchString(name) {
		http.Error(w, "document names are 1-64 characters of [A-Za-z0-9_-]", http.StatusBadRequest)
		return nil
	}
	h, err := s.hub(name)
	if err != nil {
		s.log.Error("open document", "doc", name, "err", err)
		http.Error(w, "document unavailable", http.StatusServiceUnavailable)
		return nil
	}
	return h
}

// serveDoc returns the server's materialized text, e.g. for curl.
func (s *Server) serveDoc(w http.ResponseWriter, r *http.Request) {
	h := s.hubFor(w, r.PathValue("name"))
	if h == nil {
		return
	}
	text, ok := h.text(r.Context())
	if !ok {
		http.Error(w, "document unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	io.WriteString(w, text)
}

// serveWS runs one connection: a writer goroutine drains peer.send, this
// goroutine reads. Unlike gorilla there is no per-connection "one reader, one
// writer" bookkeeping beyond that: coder/websocket's Write is safe to call
// concurrently and every call takes a context, so cancelling ctx is the one
// way to tear the connection down, whoever decides it (hub, writer, reader).
func (s *Server) serveWS(w http.ResponseWriter, r *http.Request) {
	h := s.hubFor(w, r.URL.Query().Get("doc"))
	if h == nil {
		return
	}
	// Accept refuses cross-origin browser requests by default (chathub
	// overrides gorilla's equivalent default with "allow all").
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer conn.CloseNow()
	conn.SetReadLimit(maxMessage)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	var hello wire.Message
	if err := wsjson.Read(ctx, conn, &hello); err != nil || hello.Type != wire.Hello || !rga.ValidReplica(hello.Replica) {
		conn.Close(websocket.StatusPolicyViolation, "first message must be a hello with a valid replica")
		return
	}

	p := &peer{replica: hello.Replica, send: make(chan wire.Message, sendBuffer), kick: cancel}
	var writer sync.WaitGroup
	writer.Go(func() {
		defer cancel()
		for {
			select {
			case m := <-p.send:
				if err := wsjson.Write(ctx, conn, m); err != nil {
					return
				}
			case <-ctx.Done():
				return
			}
		}
	})

	if deliver(ctx, h, h.joins, join{p, hello.Version}) {
		for {
			var m wire.Message
			if err := wsjson.Read(ctx, conn, &m); err != nil || m.Type != wire.Ops {
				break
			}
			if !deliver(ctx, h, h.in, inbound{p, m.Ops}) {
				break
			}
		}
		select {
		case h.leaves <- p:
		case <-h.done:
		}
	}
	cancel()
	writer.Wait()
}

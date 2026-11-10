package serve

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"path"
	"slices"
	"strings"
	"time"

	"sitegen/internal/site"
)

//go:embed reload.js
var reloadJS string

var reloadScript = "<script>\n" + reloadJS + "</script>\n"

// inject puts the reload script before </body>. It runs at response time, on
// the in-memory copy, so files written by `sitegen build` never contain it.
func inject(page []byte) []byte {
	i := bytes.LastIndex(page, []byte("</body>"))
	if i < 0 {
		i = len(page)
	}
	return slices.Concat(page[:i], []byte(reloadScript), page[i:])
}

type Server struct {
	src     fs.FS
	builder *site.Builder
	out     *site.MemOutput
	hub     *Hub
	log     io.Writer
}

func New(src fs.FS, b *site.Builder, log io.Writer) *Server {
	return &Server{src: src, builder: b, out: site.NewMemOutput(), hub: NewHub(), log: log}
}

// Rebuild builds into memory and tells the browsers. A failed build keeps the
// previous output being served and puts the error on every open page instead.
// Calls must not overlap (the Builder is not concurrency-safe); Run serialises them.
func (s *Server) Rebuild(ctx context.Context) error {
	start := time.Now()
	st, err := s.builder.Build(ctx, s.src, s.out)
	if err != nil {
		s.hub.Fail(err)
		return err
	}
	s.hub.Reload()
	fmt.Fprintf(s.log, "built in %s: %d rendered, %d unchanged, %d removed\n",
		time.Since(start).Round(time.Millisecond), st.Rendered, st.Skipped, st.Removed)
	return nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /__reload", s.hub)
	mux.HandleFunc("GET /", s.page)
	return mux
}

func (s *Server) page(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/")
	if name == "" || strings.HasSuffix(name, "/") {
		name += "index.html"
	}
	data, ok := s.out.Get(name)
	if !ok {
		if _, isDir := s.out.Get(name + "/index.html"); isDir {
			http.Redirect(w, r, r.URL.Path+"/", http.StatusMovedPermanently)
			return
		}
		http.NotFound(w, r)
		return
	}
	if path.Ext(name) == ".html" {
		data = inject(data)
	}
	w.Header().Set("Cache-Control", "no-store")
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(data))
}

// Run serves on ln and rebuilds on every signal from changes until ctx is done
// or changes is closed.
func (s *Server) Run(ctx context.Context, ln net.Listener, changes <-chan struct{}) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	// SSE responses never finish on their own, and Shutdown does not cancel
	// request contexts. Deriving them from ctx is what lets Shutdown return.
	srv := &http.Server{Handler: s.Handler(), BaseContext: func(net.Listener) context.Context { return ctx }}
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()

loop:
	for {
		select {
		case err := <-serveErr:
			return err
		case <-ctx.Done():
			break loop
		case _, ok := <-changes:
			if !ok {
				break loop
			}
			if err := s.Rebuild(ctx); err != nil && ctx.Err() == nil {
				fmt.Fprintf(s.log, "build failed (still serving the last good site):\n%v\n", err)
			}
		}
	}
	cancel()
	sctx, stop := context.WithTimeout(context.Background(), 3*time.Second)
	defer stop()
	return srv.Shutdown(sctx)
}

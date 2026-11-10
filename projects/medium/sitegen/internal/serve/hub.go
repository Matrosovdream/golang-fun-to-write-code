// Package serve is the dev server: it serves the in-memory build, injects a
// live-reload script into HTML, and tells open browsers when to refresh.
package serve

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
)

type event struct{ name, data string }

// write emits one Server-Sent Event: "event:" and "data:" lines, then a blank
// line. A multi-line payload needs one "data:" prefix per line.
func (e event) write(w io.Writer) {
	fmt.Fprintf(w, "event: %s\n", e.name)
	for line := range strings.SplitSeq(e.data, "\n") {
		fmt.Fprintf(w, "data: %s\n", line)
	}
	fmt.Fprint(w, "\n")
}

// Hub fans build results out to every connected browser. It is also the SSE
// endpoint (/__reload).
type Hub struct {
	mu      sync.Mutex
	subs    map[chan event]struct{}
	lastErr string // replayed to new subscribers until the next good build
}

func NewHub() *Hub { return &Hub{subs: map[chan event]struct{}{}} }

func (h *Hub) Reload()        { h.publish(event{"reload", ""}) }
func (h *Hub) Fail(err error) { h.publish(event{"build-error", err.Error()}) }

func (h *Hub) publish(ev event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.lastErr = ""
	if ev.name == "build-error" {
		h.lastErr = ev.data
	}
	for ch := range h.subs {
		select {
		case ch <- ev:
		default: // a stalled client already has events queued; never block the build
		}
	}
}

func (h *Hub) subscribe() (<-chan event, func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	ch := make(chan event, 4)
	if h.lastErr != "" {
		ch <- event{"build-error", h.lastErr}
	}
	h.subs[ch] = struct{}{}
	return ch, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		delete(h.subs, ch)
	}
}

func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	events, unsubscribe := h.subscribe()
	defer unsubscribe()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	fmt.Fprint(w, ": connected\n\n") // a comment line: gets headers to the browser now
	flusher.Flush()

	for {
		select {
		case ev := <-events:
			ev.write(w)
			flusher.Flush()
		case <-r.Context().Done(): // browser closed the tab, or the server is stopping
			return
		}
	}
}

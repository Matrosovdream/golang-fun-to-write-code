// Package hub implements a multi-room chat over WebSockets.
//
// Architecture: every connection gets a read pump and a write pump; ALL
// state (rooms, membership, history) is owned by the single Run goroutine.
// Pumps and handlers never touch maps — they send events. This is what
// makes the hub race-free without a single mutex.
package hub

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

const historySize = 32

type event struct {
	client *Client
	env    Envelope
}

type statsRequest struct {
	reply chan []RoomStat // request/response over a channel: how outsiders read hub state
}

type RoomStat struct {
	Name    string `json:"name"`
	Clients int    `json:"clients"`
}

type room struct {
	clients map[*Client]struct{}
	history *ring
}

type Hub struct {
	register   chan *Client
	unregister chan *Client
	events     chan event
	stats      chan statsRequest
	shutdown   chan struct{}
	done       chan struct{}

	rooms map[string]*room
	log   *slog.Logger
}

func New(log *slog.Logger) *Hub {
	return &Hub{
		register:   make(chan *Client),
		unregister: make(chan *Client),
		events:     make(chan event),
		stats:      make(chan statsRequest),
		shutdown:   make(chan struct{}),
		done:       make(chan struct{}),
		rooms:      make(map[string]*room),
		log:        log,
	}
}

// Run is the hub's event loop — the only goroutine allowed to touch h.rooms.
func (h *Hub) Run() {
	defer close(h.done)
	for {
		select {
		case c := <-h.register:
			h.joinRoom(c, c.room)
		case c := <-h.unregister:
			h.leaveRoom(c, "disconnected")
		case ev := <-h.events:
			h.handle(ev)
		case req := <-h.stats:
			out := make([]RoomStat, 0, len(h.rooms))
			for name, r := range h.rooms {
				out = append(out, RoomStat{Name: name, Clients: len(r.clients)})
			}
			req.reply <- out
		case <-h.shutdown:
			for name := range h.rooms {
				h.broadcast(name, info(name, "server shutting down"))
			}
			for _, r := range h.rooms {
				for c := range r.clients {
					close(c.send) // writePump sends a close frame and exits
				}
			}
			return
		}
	}
}

// Shutdown asks the loop to drain everything and waits for it (or ctx).
func (h *Hub) Shutdown(ctx context.Context) error {
	close(h.shutdown)
	select {
	case <-h.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (h *Hub) handle(ev event) {
	c := ev.client
	switch ev.env.Type {
	case "msg":
		if c.room == "" {
			h.trySend(c, errMsg("join a room first"))
			return
		}
		msg := Envelope{Type: "msg", Room: c.room, From: c.name, Text: ev.env.Text, At: time.Now()}
		h.rooms[c.room].history.add(msg)
		h.broadcast(c.room, msg)
	case "join":
		if ev.env.Room == "" {
			h.trySend(c, errMsg("room name required"))
			return
		}
		h.leaveRoom(c, "left the room")
		h.joinRoom(c, ev.env.Room)
	case "nick":
		old := c.name
		c.name = ev.env.Name
		if c.room != "" {
			h.broadcast(c.room, info(c.room, fmt.Sprintf("%s is now %s", old, c.name)))
		}
	default:
		h.trySend(c, errMsg("unknown message type "+ev.env.Type))
	}
}

func (h *Hub) joinRoom(c *Client, name string) {
	if name == "" {
		return
	}
	r, ok := h.rooms[name]
	if !ok {
		r = &room{clients: make(map[*Client]struct{}), history: newRing(historySize)}
		h.rooms[name] = r
	}
	r.clients[c] = struct{}{}
	c.room = name

	// Replay history to the newcomer; announce the join to everyone else —
	// the joiner doesn't need to hear about themselves.
	h.trySend(c, Envelope{Type: "history", Room: name, Messages: r.history.list()})
	h.broadcastExcept(name, info(name, c.name+" joined"), c)
}

func (h *Hub) leaveRoom(c *Client, reason string) {
	r, ok := h.rooms[c.room]
	if !ok {
		return
	}
	delete(r.clients, c)
	if len(r.clients) == 0 {
		delete(h.rooms, c.room) // empty rooms (and their history) are garbage
	} else {
		h.broadcast(c.room, info(c.room, fmt.Sprintf("%s %s", c.name, reason)))
	}
	c.room = ""
}

func (h *Hub) broadcast(roomName string, env Envelope) {
	h.broadcastExcept(roomName, env, nil)
}

func (h *Hub) broadcastExcept(roomName string, env Envelope, except *Client) {
	r, ok := h.rooms[roomName]
	if !ok {
		return
	}
	for c := range r.clients {
		if c != except {
			h.trySend(c, env)
		}
	}
}

// trySend never blocks the hub loop: a client whose buffer is full is a slow
// consumer, and one stuck client must not stall the whole chat. Dropping the
// connection (not the message) keeps delivery in-order for everyone else.
func (h *Hub) trySend(c *Client, env Envelope) {
	select {
	case c.send <- env:
	default:
		h.log.Warn("dropping slow consumer", "client", c.name)
		delete(h.rooms[c.room].clients, c)
		close(c.send)
	}
}

// Stats is safe to call from any goroutine.
func (h *Hub) Stats() []RoomStat {
	req := statsRequest{reply: make(chan []RoomStat, 1)}
	h.stats <- req
	return <-req.reply
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	// Demo server: accept any origin. In production, check it — this is the
	// only CSRF-style defense a WebSocket endpoint has.
	CheckOrigin: func(*http.Request) bool { return true },
}

// ServeWS upgrades an HTTP request and hands the connection to the hub.
func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return // Upgrade already replied with an HTTP error
	}
	name := r.URL.Query().Get("nick")
	if name == "" {
		name = "anon-" + conn.RemoteAddr().String()
	}
	c := &Client{
		hub:  h,
		conn: conn,
		send: make(chan Envelope, sendBuffer),
		name: name,
		room: r.URL.Query().Get("room"),
	}

	h.register <- c
	go c.writePump()
	go c.readPump()
}

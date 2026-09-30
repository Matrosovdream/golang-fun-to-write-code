package hub

import (
	"log/slog"
	"time"

	"github.com/gorilla/websocket"
)

const (
	writeWait  = 10 * time.Second
	pongWait   = 60 * time.Second
	pingPeriod = (pongWait * 9) / 10 // must fire before the pong deadline

	// sendBuffer absorbs broadcast bursts while the socket drains. Size it
	// for your worst-case burst: too small and busy rooms drop clients that
	// are merely momentarily behind, not actually dead.
	sendBuffer = 256
)

// Client is one WebSocket connection. Two goroutines per client — readPump
// and writePump — because gorilla/websocket allows at most one concurrent
// reader and one concurrent writer per connection.
type Client struct {
	hub  *Hub
	conn *websocket.Conn
	send chan Envelope
	name string
	room string
}

// readPump is the only goroutine reading the socket. Every inbound message
// is funneled into the hub's single event loop.
func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()

	c.conn.SetReadLimit(4096)
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	// Each pong pushes the read deadline forward; a dead peer stops ponging
	// and the next Read fails instead of hanging forever.
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(pongWait))
	})

	for {
		var env Envelope
		if err := c.conn.ReadJSON(&env); err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				slog.Debug("read error", "client", c.name, "err", err)
			}
			return
		}
		c.hub.events <- event{client: c, env: env}
	}
}

// writePump is the only goroutine writing the socket: it drains c.send and
// keeps the connection alive with pings.
func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case env, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// The hub closed our channel — say goodbye properly.
				_ = c.conn.WriteMessage(websocket.CloseMessage,
					websocket.FormatCloseMessage(websocket.CloseNormalClosure, "server closing"))
				return
			}
			if err := c.conn.WriteJSON(env); err != nil {
				return
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

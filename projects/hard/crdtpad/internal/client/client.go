// Package client is a headless replica: it owns an rga.Doc, applies local
// edits instantly, and syncs with a server whenever it is connected. Edits
// made while offline simply accumulate in the Doc and flow out on reconnect.
package client

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"sync"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"crdtpad/internal/rga"
	"crdtpad/internal/wire"
)

const (
	maxMessage = 64 << 20 // a sync message carries the whole document
	chunk      = 256      // ops per outgoing message, to stay under the server's limit
)

type Client struct {
	replica string
	wsURL   string

	mu   sync.Mutex // guards everything below
	doc  *rga.Doc
	conn *websocket.Conn // nil while offline
	ctx  context.Context // context of the live connection
	done chan struct{}   // closed when the live connection's read loop exits
}

// New prepares a replica for document doc on the server at baseURL
// (http://host:port). The replica name must be unique per Client lifetime.
func New(replica, baseURL, doc string) *Client {
	return &Client{
		replica: replica,
		wsURL:   baseURL + "/ws?doc=" + url.QueryEscape(doc),
		doc:     rga.New(replica),
	}
}

// Connect dials the server and starts syncing in the background until ctx is
// cancelled, Disconnect is called, or the connection breaks.
func (c *Client) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		return errors.New("client: already connected")
	}
	conn, _, err := websocket.Dial(ctx, c.wsURL, nil)
	if err != nil {
		return fmt.Errorf("client: dial: %w", err)
	}
	conn.SetReadLimit(maxMessage)
	hello := wire.Message{Type: wire.Hello, Replica: c.replica, Version: c.doc.Version()}
	if err := wsjson.Write(ctx, conn, hello); err != nil {
		conn.CloseNow()
		return fmt.Errorf("client: hello: %w", err)
	}
	c.conn, c.ctx, c.done = conn, ctx, make(chan struct{})
	go c.readLoop(ctx, conn, c.done)
	return nil
}

// Disconnect closes the connection and waits until no more remote ops can
// arrive. The replica keeps working offline.
func (c *Client) Disconnect() {
	c.mu.Lock()
	conn, done := c.conn, c.done
	c.conn = nil
	c.mu.Unlock()
	if conn != nil {
		conn.Close(websocket.StatusNormalClosure, "")
		<-done
	}
}

func (c *Client) readLoop(ctx context.Context, conn *websocket.Conn, done chan struct{}) {
	defer close(done)
	defer conn.CloseNow()
	defer func() {
		c.mu.Lock()
		if c.conn == conn {
			c.conn = nil
		}
		c.mu.Unlock()
	}()
	for {
		var m wire.Message
		if err := wsjson.Read(ctx, conn, &m); err != nil {
			return
		}
		c.mu.Lock()
		failed := false
		for _, op := range m.Ops {
			if _, err := c.doc.Apply(op); err != nil {
				failed = true
				break
			}
		}
		if m.Type == wire.Sync && !failed {
			// The server told us what it has; send what it lacks.
			for ops := range slices.Chunk(c.doc.Missing(m.Version), chunk) {
				c.send(ctx, conn, ops)
			}
		}
		c.mu.Unlock()
		if failed {
			return
		}
	}
}

// send must be called with c.mu held, which keeps one replica's ops in order
// on the wire. Failures are ignored on purpose: the op is already in the Doc,
// and the next sync after a reconnect resends everything the server lacks.
func (c *Client) send(ctx context.Context, conn *websocket.Conn, ops []rga.Op) {
	if err := wsjson.Write(ctx, conn, wire.Message{Type: wire.Ops, Ops: ops}); err != nil {
		conn.CloseNow()
	}
}

func (c *Client) Insert(pos int, char rune) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	op, err := c.doc.Insert(pos, char)
	if err == nil && c.conn != nil {
		c.send(c.ctx, c.conn, []rga.Op{op})
	}
	return err
}

func (c *Client) Delete(pos int) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	op, err := c.doc.Delete(pos)
	if err == nil && c.conn != nil {
		c.send(c.ctx, c.conn, []rga.Op{op})
	}
	return err
}

func (c *Client) Text() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.doc.Text()
}

func (c *Client) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.doc.Len()
}

func (c *Client) Version() rga.Version {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.doc.Version()
}

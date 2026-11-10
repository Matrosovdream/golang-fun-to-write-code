// Package bus runs the NATS server inside the process and wires services to it.
package bus

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"orderflow/internal/inbox"
	"orderflow/internal/outbox"
	"orderflow/internal/sqlite"
)

// Each stream captures "<name>.>". orders, inventory and payments hold the
// messages addressed to that service; dlq holds what could not be processed.
var Streams = []string{"orders", "inventory", "payments", "dlq"}

type Bus struct {
	Server *server.Server
	Conn   *nats.Conn
	JS     jetstream.JetStream
}

// Start boots a JetStream-enabled server on a random localhost port, stores
// its data under dir, connects a client and creates the streams.
func Start(ctx context.Context, dir string) (*Bus, error) {
	ns, err := server.NewServer(&server.Options{
		Host: "127.0.0.1", Port: -1, JetStream: true, StoreDir: dir, NoLog: true, NoSigs: true,
	})
	if err != nil {
		return nil, fmt.Errorf("nats server: %w", err)
	}
	go ns.Start()
	if !ns.ReadyForConnections(10 * time.Second) {
		ns.Shutdown()
		return nil, fmt.Errorf("nats server not ready")
	}
	b := &Bus{Server: ns}
	if b.Conn, err = nats.Connect(ns.ClientURL()); err != nil {
		b.Close()
		return nil, fmt.Errorf("connect: %w", err)
	}
	if b.JS, err = jetstream.New(b.Conn); err != nil {
		b.Close()
		return nil, err
	}
	for _, name := range Streams {
		_, err := b.JS.CreateOrUpdateStream(ctx, jetstream.StreamConfig{Name: name, Subjects: []string{name + ".>"}})
		if err != nil {
			b.Close()
			return nil, fmt.Errorf("create stream %s: %w", name, err)
		}
	}
	return b, nil
}

func (b *Bus) Close() {
	if b.Conn != nil {
		b.Conn.Close()
	}
	b.Server.Shutdown()
	b.Server.WaitForShutdown()
}

// Node is what every service has in common: its own SQLite file, a relay
// that publishes its outbox and a consumer that drains its stream.
type Node struct {
	DB       *sql.DB
	Relay    *outbox.Relay
	Consumer *inbox.Consumer
}

// NewNode opens the database at path and creates schema plus the outbox and
// inbox tables. js may be nil for code that never calls Run.
func NewNode(path, schema string, js jetstream.JetStream, stream string, h inbox.Handler) (*Node, error) {
	db, err := sqlite.Open(path)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(schema + outbox.Schema + inbox.Schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate %s: %w", stream, err)
	}
	return &Node{DB: db, Relay: outbox.NewRelay(db, js), Consumer: inbox.NewConsumer(db, js, stream, h)}, nil
}

func (n *Node) Close() error { return n.DB.Close() }

// Run blocks until ctx is done or the consumer cannot start; the relay is
// always stopped before it returns.
func (n *Node) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	defer wg.Wait()
	defer cancel()
	wg.Go(func() { n.Relay.Run(ctx) })
	return n.Consumer.Run(ctx)
}

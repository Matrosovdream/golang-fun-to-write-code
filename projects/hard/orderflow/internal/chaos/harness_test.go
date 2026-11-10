// Package chaos holds the end-to-end fault-injection tests: the three
// services and a real embedded NATS server in one process, with faults
// injected through the Relay and Consumer hooks.
package chaos

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"orderflow/internal/bus"
	"orderflow/internal/check"
	"orderflow/internal/inbox"
	"orderflow/internal/inventory"
	"orderflow/internal/orders"
	"orderflow/internal/outbox"
	"orderflow/internal/payments"
)

var errInjected = errors.New("injected fault")

// system is the whole pipeline in one process: a fresh embedded broker and
// three services with their own SQLite files, all under t.TempDir().
type system struct {
	bus *bus.Bus
	ord *orders.Service
	inv *inventory.Service
	pay *payments.Service
}

func newSystem(t *testing.T, stock map[string]int) *system {
	t.Helper()
	dir := t.TempDir()
	b, err := bus.Start(t.Context(), filepath.Join(dir, "nats"))
	require.NoError(t, err)
	t.Cleanup(b.Close)

	s := &system{bus: b}
	s.ord, err = orders.New(filepath.Join(dir, "orders.db"), b.JS)
	require.NoError(t, err)
	s.inv, err = inventory.New(filepath.Join(dir, "inventory.db"), b.JS, stock)
	require.NoError(t, err)
	s.pay, err = payments.New(filepath.Join(dir, "payments.db"), b.JS)
	require.NoError(t, err)
	t.Cleanup(func() { s.ord.Close(); s.inv.Close(); s.pay.Close() })

	for _, n := range s.nodes() {
		n.Relay.Interval = 10 * time.Millisecond
		n.Consumer.AckWait = time.Second
		n.Consumer.RetryBase = 10 * time.Millisecond
		n.Consumer.MaxDeliver = 12
	}
	return s
}

func (s *system) nodes() []*bus.Node { return []*bus.Node{s.ord.Node, s.inv.Node, s.pay.Node} }

func (s *system) dbs() []*sql.DB { return []*sql.DB{s.ord.DB, s.inv.DB, s.pay.DB} }

// noDedupe strips Nats-Msg-Id, simulating a broker whose dedupe window has
// already expired — so only the consumers' inboxes can catch a duplicate.
type noDedupe struct{ outbox.Publisher }

func (n noDedupe) PublishMsg(ctx context.Context, m *nats.Msg, opts ...jetstream.PublishOpt) (*jetstream.PubAck, error) {
	m.Header.Del(nats.MsgIdHdr)
	return n.Publisher.PublishMsg(ctx, m, opts...)
}

func (s *system) disableBrokerDedupe() {
	for _, n := range s.nodes() {
		n.Relay.Pub = noDedupe{n.Relay.Pub}
	}
}

func (s *system) start(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	for _, n := range s.nodes() {
		wg.Go(func() { assert.NoError(t, n.Run(ctx)) })
	}
	t.Cleanup(func() { cancel(); wg.Wait() })
}

func (s *system) place(t *testing.T, sku string, qty int) string {
	t.Helper()
	o, err := s.ord.Create(t.Context(), orders.CreateRequest{SKU: sku, Qty: qty, Amount: 100 * qty}, "")
	require.NoError(t, err)
	return o.ID
}

// await waits for the given orders to reach a terminal state, then for every
// outbox to drain. The timeout only bounds a hang; it is not a delay.
func (s *system) await(t *testing.T, ids ...string) {
	t.Helper()
	require.Eventually(t, func() bool {
		for _, id := range ids {
			if o, err := s.ord.Get(t.Context(), id); err != nil || !o.State.Terminal() {
				return false
			}
		}
		return true
	}, 90*time.Second, 20*time.Millisecond, "orders did not all reach a terminal state")
	require.Eventually(t, func() bool {
		for _, db := range s.dbs() {
			var n int
			if err := db.QueryRow(`SELECT COUNT(*) FROM outbox WHERE sent_at IS NULL`).Scan(&n); err != nil || n > 0 {
				return false
			}
		}
		return true
	}, 90*time.Second, 20*time.Millisecond, "outboxes did not drain")
}

// awaitDuplicates waits until consumers have recognised at least n
// redeliveries. A lost ack is only redelivered after AckWait, so this is what
// separates "the order finished" from "the system went quiet".
func (s *system) awaitDuplicates(t *testing.T, n int64) {
	t.Helper()
	require.Eventually(t, func() bool { return s.duplicates() >= n }, 90*time.Second, 20*time.Millisecond)
}

func (s *system) order(t *testing.T, id string) orders.Order {
	t.Helper()
	o, err := s.ord.Get(t.Context(), id)
	require.NoError(t, err)
	return o
}

func (s *system) stock(t *testing.T, sku string) inventory.Stock {
	t.Helper()
	all, err := s.inv.Stock(t.Context())
	require.NoError(t, err)
	for _, st := range all {
		if st.SKU == sku {
			return st
		}
	}
	require.FailNow(t, "unknown sku "+sku)
	return inventory.Stock{}
}

func (s *system) requireInvariants(t *testing.T) {
	t.Helper()
	all, err := s.ord.List(t.Context())
	require.NoError(t, err)
	st, err := s.inv.Stock(t.Context())
	require.NoError(t, err)
	require.Empty(t, check.Invariants(all, st))
}

func (s *system) count(t *testing.T, db *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	require.NoError(t, db.QueryRow(query, args...).Scan(&n))
	return n
}

func (s *system) sum(f func(*inbox.Consumer) *atomic.Int64) int64 {
	var total int64
	for _, n := range s.nodes() {
		total += f(n.Consumer).Load()
	}
	return total
}

func (s *system) processed() int64 {
	return s.sum(func(c *inbox.Consumer) *atomic.Int64 { return &c.Processed })
}
func (s *system) duplicates() int64 {
	return s.sum(func(c *inbox.Consumer) *atomic.Int64 { return &c.Duplicates })
}
func (s *system) deadLetters() int64 {
	return s.sum(func(c *inbox.Consumer) *atomic.Int64 { return &c.DeadLettered })
}

// once reports true the first time it sees an ID: "fail once per event".
type once struct {
	mu   sync.Mutex
	seen map[string]bool
}

func (o *once) first(id string) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.seen == nil {
		o.seen = map[string]bool{}
	}
	fresh := !o.seen[id]
	o.seen[id] = true
	return fresh
}

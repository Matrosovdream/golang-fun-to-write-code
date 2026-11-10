package orders

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/nats-io/nats.go/jetstream"

	"orderflow/internal/bus"
	"orderflow/internal/event"
	"orderflow/internal/outbox"
	"orderflow/internal/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS orders (
	id     TEXT PRIMARY KEY,
	sku    TEXT NOT NULL,
	qty    INTEGER NOT NULL,
	amount INTEGER NOT NULL,
	state  TEXT NOT NULL,
	reason TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS history (
	seq      INTEGER PRIMARY KEY AUTOINCREMENT,
	order_id TEXT NOT NULL,
	state    TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS history_order ON history (order_id);`

var (
	ErrInvalid  = errors.New("invalid order")
	ErrNotFound = errors.New("order not found")
)

type Order struct {
	ID      string  `json:"id"`
	SKU     string  `json:"sku"`
	Qty     int     `json:"qty"`
	Amount  int     `json:"amount"`
	State   State   `json:"state"`
	Reason  string  `json:"reason,omitempty"`
	History []State `json:"history,omitempty"`
}

type CreateRequest struct {
	SKU    string `json:"sku"`
	Qty    int    `json:"qty"`
	Amount int    `json:"amount"`
}

type Service struct{ *bus.Node }

// New opens (and migrates) the service's database. js may be nil when the
// service is only used through Create and the read methods.
func New(path string, js jetstream.JetStream) (*Service, error) {
	s := &Service{}
	n, err := bus.NewNode(path, schema, js, "orders", s.handle)
	if err != nil {
		return nil, err
	}
	s.Node = n
	return s, nil
}

// Create stores the order and its first command atomically: either both
// exist or neither does, and the relay publishes the command whenever it
// next polls — even if the broker is down right now.
func (s *Service) Create(ctx context.Context, req CreateRequest, correlationID string) (Order, error) {
	if req.SKU == "" || req.Qty <= 0 || req.Amount <= 0 {
		return Order{}, fmt.Errorf("%w: sku, qty and amount are required, qty and amount positive", ErrInvalid)
	}
	o := Order{ID: event.NewID("ord"), SKU: req.SKU, Qty: req.Qty, Amount: req.Amount, State: Pending}
	correlationID = cmp.Or(correlationID, event.NewID("cor"))
	err := sqlite.InTx(ctx, s.DB, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO orders (id, sku, qty, amount, state) VALUES (?, ?, ?, ?, ?)`,
			o.ID, o.SKU, o.Qty, o.Amount, o.State)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO history (order_id, state) VALUES (?, ?)`, o.ID, o.State); err != nil {
			return err
		}
		return outbox.Send(ctx, tx, event.Reserve, o.ID, correlationID, o.cmd())
	})
	if err != nil {
		return Order{}, fmt.Errorf("create order: %w", err)
	}
	return o, nil
}

func (o Order) cmd() event.Cmd {
	return event.Cmd{OrderID: o.ID, SKU: o.SKU, Qty: o.Qty, Amount: o.Amount}
}

// handle is the orchestrator: one reply in, one state change and at most one
// command out, all inside the inbox transaction.
func (s *Service) handle(ctx context.Context, tx *sql.Tx, ev event.Event) error {
	r, ok := reactions[ev.Type]
	if !ok {
		return fmt.Errorf("unexpected event type %q", ev.Type)
	}
	rep, err := event.Data[event.Reply](ev)
	if err != nil {
		return err
	}
	o, err := load(ctx, tx, rep.OrderID)
	if err != nil {
		return err
	}
	if err := Transition(o.State, r.to); err != nil {
		return fmt.Errorf("order %s on %s: %w", o.ID, ev.Type, err)
	}
	o.State, o.Reason = r.to, cmp.Or(rep.Reason, o.Reason)
	if _, err := tx.ExecContext(ctx, `UPDATE orders SET state = ?, reason = ? WHERE id = ?`, o.State, o.Reason, o.ID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO history (order_id, state) VALUES (?, ?)`, o.ID, o.State); err != nil {
		return err
	}
	if r.next == "" {
		return nil
	}
	return outbox.Send(ctx, tx, r.next, o.ID, ev.CorrelationID, o.cmd())
}

type scanner interface{ Scan(...any) error }

func scan(r scanner) (o Order, err error) {
	err = r.Scan(&o.ID, &o.SKU, &o.Qty, &o.Amount, &o.State, &o.Reason)
	return o, err
}

const selectOrder = `SELECT id, sku, qty, amount, state, reason FROM orders`

func load(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, id string) (Order, error) {
	o, err := scan(q.QueryRowContext(ctx, selectOrder+` WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return o, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	return o, err
}

// Get returns one order with its full state history.
func (s *Service) Get(ctx context.Context, id string) (Order, error) {
	o, err := load(ctx, s.DB, id)
	if err != nil {
		return o, err
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT state FROM history WHERE order_id = ? ORDER BY seq`, id)
	if err != nil {
		return o, err
	}
	defer rows.Close()
	for rows.Next() {
		var st State
		if err := rows.Scan(&st); err != nil {
			return o, err
		}
		o.History = append(o.History, st)
	}
	return o, rows.Err()
}

// List returns all orders, oldest first, without history.
func (s *Service) List(ctx context.Context) ([]Order, error) {
	rows, err := s.DB.QueryContext(ctx, selectOrder+` ORDER BY rowid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Order{}
	for rows.Next() {
		o, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

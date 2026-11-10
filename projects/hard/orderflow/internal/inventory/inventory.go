// Package inventory owns finite stock per SKU.
//
// Every unit is in exactly one bucket: available, reserved (held for an
// order whose payment is pending) or sold. The CHECK constraints make a
// negative bucket impossible even if a handler has a bug.
package inventory

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/nats-io/nats.go/jetstream"

	"orderflow/internal/bus"
	"orderflow/internal/event"
	"orderflow/internal/outbox"
)

const schema = `
CREATE TABLE IF NOT EXISTS stock (
	sku       TEXT PRIMARY KEY,
	initial   INTEGER NOT NULL,
	available INTEGER NOT NULL CHECK (available >= 0),
	reserved  INTEGER NOT NULL CHECK (reserved >= 0),
	sold      INTEGER NOT NULL CHECK (sold >= 0)
);
CREATE TABLE IF NOT EXISTS reservations (
	order_id TEXT PRIMARY KEY,
	sku      TEXT NOT NULL,
	qty      INTEGER NOT NULL,
	state    TEXT NOT NULL CHECK (state IN ('RESERVED', 'COMMITTED', 'RELEASED'))
);`

type Stock struct {
	SKU       string `json:"sku"`
	Initial   int    `json:"initial"`
	Available int    `json:"available"`
	Reserved  int    `json:"reserved"`
	Sold      int    `json:"sold"`
}

type Service struct{ *bus.Node }

// New opens the database and seeds stock for SKUs it has not seen before
// (a restart keeps the counts it already had).
func New(path string, js jetstream.JetStream, initial map[string]int) (*Service, error) {
	s := &Service{}
	n, err := bus.NewNode(path, schema, js, "inventory", s.handle)
	if err != nil {
		return nil, err
	}
	s.Node = n
	for sku, n := range initial {
		_, err := s.DB.Exec(`INSERT OR IGNORE INTO stock (sku, initial, available, reserved, sold) VALUES (?1, ?2, ?2, 0, 0)`, sku, n)
		if err != nil {
			s.Close()
			return nil, fmt.Errorf("seed %s: %w", sku, err)
		}
	}
	return s, nil
}

func (s *Service) Stock(ctx context.Context) ([]Stock, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT sku, initial, available, reserved, sold FROM stock ORDER BY sku`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Stock{}
	for rows.Next() {
		var st Stock
		if err := rows.Scan(&st.SKU, &st.Initial, &st.Available, &st.Reserved, &st.Sold); err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

// StockHandler serves the stock table as JSON, for the placeorders invariant
// check; it is not part of the saga.
func (s *Service) StockHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		st, err := s.Stock(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(st)
	})
}

func (s *Service) handle(ctx context.Context, tx *sql.Tx, ev event.Event) error {
	cmd, err := event.Data[event.Cmd](ev)
	if err != nil {
		return err
	}
	switch ev.Type {
	case event.Reserve:
		return reserve(ctx, tx, ev, cmd)
	case event.Commit:
		return settle(ctx, tx, ev, cmd, "COMMITTED",
			`UPDATE stock SET reserved = reserved - ?1, sold = sold + ?1 WHERE sku = ?2`, event.StockCommitted)
	case event.Release:
		return settle(ctx, tx, ev, cmd, "RELEASED",
			`UPDATE stock SET reserved = reserved - ?1, available = available + ?1 WHERE sku = ?2`, event.StockReleased)
	default:
		return fmt.Errorf("unexpected event type %q", ev.Type)
	}
}

// reserve moves units from available to reserved in one conditional UPDATE:
// the "enough stock?" check and the decrement cannot be separated by another
// writer. Running out of stock is a normal outcome, not an error.
func reserve(ctx context.Context, tx *sql.Tx, ev event.Event, cmd event.Cmd) error {
	res, err := tx.ExecContext(ctx,
		`UPDATE stock SET available = available - ?1, reserved = reserved + ?1 WHERE sku = ?2 AND available >= ?1 AND ?1 > 0`,
		cmd.Qty, cmd.SKU)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return reply(ctx, tx, ev, event.StockRejected, cmd.OrderID, "insufficient stock for "+cmd.SKU)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO reservations (order_id, sku, qty, state) VALUES (?, ?, ?, 'RESERVED')`,
		cmd.OrderID, cmd.SKU, cmd.Qty)
	if err != nil {
		return err
	}
	return reply(ctx, tx, ev, event.StockReserved, cmd.OrderID, "")
}

// settle ends a reservation. The RESERVED guard makes committing or
// releasing an order twice (or one that never reserved) an error rather
// than a second stock movement.
func settle(ctx context.Context, tx *sql.Tx, ev event.Event, cmd event.Cmd, to, moveStock, replyType string) error {
	var sku string
	var qty int
	err := tx.QueryRowContext(ctx,
		`UPDATE reservations SET state = ? WHERE order_id = ? AND state = 'RESERVED' RETURNING sku, qty`,
		to, cmd.OrderID).Scan(&sku, &qty)
	if err != nil {
		return fmt.Errorf("%s order %s: %w", to, cmd.OrderID, err)
	}
	if _, err := tx.ExecContext(ctx, moveStock, qty, sku); err != nil {
		return err
	}
	return reply(ctx, tx, ev, replyType, cmd.OrderID, "")
}

func reply(ctx context.Context, tx *sql.Tx, cause event.Event, typ, orderID, reason string) error {
	return outbox.Send(ctx, tx, typ, cause.AggregateID, cause.CorrelationID, event.Reply{OrderID: orderID, Reason: reason})
}

// Package payments charges orders. The ledger deliberately has no unique
// constraint on order_id: if the inbox ever let a duplicate through, the test
// that counts ledger rows per order would see two.
package payments

import (
	"context"
	"database/sql"
	"fmt"
	"math/rand/v2"

	"github.com/nats-io/nats.go/jetstream"

	"orderflow/internal/bus"
	"orderflow/internal/event"
	"orderflow/internal/outbox"
)

const schema = `
CREATE TABLE IF NOT EXISTS payments (
	id       INTEGER PRIMARY KEY AUTOINCREMENT,
	order_id TEXT NOT NULL,
	amount   INTEGER NOT NULL,
	status   TEXT NOT NULL CHECK (status IN ('CHARGED', 'DECLINED'))
);`

type Service struct {
	*bus.Node

	// Decline decides whether a charge fails. nil never declines. Tests plug
	// in deterministic rules, the demo plugs in RandomDecline.
	Decline func(event.Cmd) bool
}

func New(path string, js jetstream.JetStream) (*Service, error) {
	s := &Service{}
	n, err := bus.NewNode(path, schema, js, "payments", s.handle)
	if err != nil {
		return nil, err
	}
	s.Node = n
	return s, nil
}

func RandomDecline(rate float64) func(event.Cmd) bool {
	return func(event.Cmd) bool { return rand.Float64() < rate }
}

func (s *Service) handle(ctx context.Context, tx *sql.Tx, ev event.Event) error {
	if ev.Type != event.Charge {
		return fmt.Errorf("unexpected event type %q", ev.Type)
	}
	cmd, err := event.Data[event.Cmd](ev)
	if err != nil {
		return err
	}
	status, replyType, reason := "CHARGED", event.PaymentSucceeded, ""
	if s.Decline != nil && s.Decline(cmd) {
		status, replyType, reason = "DECLINED", event.PaymentDeclined, "card declined"
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO payments (order_id, amount, status) VALUES (?, ?, ?)`,
		cmd.OrderID, cmd.Amount, status)
	if err != nil {
		return err
	}
	return outbox.Send(ctx, tx, replyType, ev.AggregateID, ev.CorrelationID, event.Reply{OrderID: cmd.OrderID, Reason: reason})
}

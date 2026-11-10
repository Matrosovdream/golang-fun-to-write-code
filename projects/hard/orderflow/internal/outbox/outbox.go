// Package outbox is the sending half of reliable messaging.
//
// A service must change its own state AND tell the world about it, but a
// database and a broker cannot share a transaction. So the event is written
// to an outbox table in the SAME transaction as the business row (Insert
// takes the *sql.Tx — there is no way to publish without one), and a relay
// publishes the rows afterwards. If the relay dies between publishing and
// marking a row sent, the row is published again: the outbox gives
// at-least-once delivery, never exactly-once. Consumers must dedupe.
//
// The relay sets the event ID as the Nats-Msg-Id header, which makes
// JetStream drop republished duplicates inside its dedupe window. That is a
// free optimisation, not a guarantee (the window is finite, 2 minutes by
// default); nothing downstream relies on it.
package outbox

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"orderflow/internal/event"
)

const Schema = `
CREATE TABLE IF NOT EXISTS outbox (
	seq     INTEGER PRIMARY KEY AUTOINCREMENT,
	payload BLOB NOT NULL,
	sent_at TEXT
);
CREATE INDEX IF NOT EXISTS outbox_unsent ON outbox (seq) WHERE sent_at IS NULL;`

func Insert(ctx context.Context, tx *sql.Tx, ev event.Event) error {
	b, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO outbox (payload) VALUES (?)`, b); err != nil {
		return fmt.Errorf("insert outbox row: %w", err)
	}
	return nil
}

// Send builds an event and Inserts it.
func Send(ctx context.Context, tx *sql.Tx, typ, aggregateID, correlationID string, data any) error {
	ev, err := event.New(typ, aggregateID, correlationID, data)
	if err != nil {
		return err
	}
	return Insert(ctx, tx, ev)
}

type Publisher interface {
	PublishMsg(ctx context.Context, msg *nats.Msg, opts ...jetstream.PublishOpt) (*jetstream.PubAck, error)
}

type Relay struct {
	DB       *sql.DB
	Pub      Publisher
	Interval time.Duration

	// AfterPublish is a fault-injection hook: returning an error simulates a
	// crash after the broker accepted the event but before the row was marked
	// sent, so the next poll publishes it again.
	AfterPublish func(event.Event) error
}

func NewRelay(db *sql.DB, pub Publisher) *Relay {
	return &Relay{DB: db, Pub: pub, Interval: 25 * time.Millisecond}
}

func (r *Relay) Run(ctx context.Context) {
	t := time.NewTicker(r.Interval)
	defer t.Stop()
	for {
		if err := r.Flush(ctx); err != nil && ctx.Err() == nil {
			slog.Warn("outbox relay", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

const batchSize = 100

// Flush publishes up to batchSize unsent rows in insertion order and stops at
// the first failure, so one stuck event never lets a later one overtake it.
// No transaction is open while publishing: the cursor is a plain WAL read
// (it never blocks the writer) and each mark-sent is its own short write.
func (r *Relay) Flush(ctx context.Context) error {
	rows, err := r.DB.QueryContext(ctx,
		`SELECT seq, payload FROM outbox WHERE sent_at IS NULL ORDER BY seq LIMIT ?`, batchSize)
	if err != nil {
		return fmt.Errorf("select unsent: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var seq int64
		var payload []byte
		if err := rows.Scan(&seq, &payload); err != nil {
			return err
		}
		if err := r.publish(ctx, seq, payload); err != nil {
			return err
		}
	}
	return rows.Err()
}

func (r *Relay) publish(ctx context.Context, seq int64, payload []byte) error {
	ev, err := event.Decode(payload)
	if err != nil {
		return fmt.Errorf("outbox row %d: %w", seq, err)
	}
	msg := &nats.Msg{Subject: ev.Type, Data: payload, Header: nats.Header{}}
	msg.Header.Set(nats.MsgIdHdr, ev.ID)
	if _, err := r.Pub.PublishMsg(ctx, msg); err != nil {
		return fmt.Errorf("publish %s %s: %w", ev.Type, ev.ID, err)
	}
	if r.AfterPublish != nil {
		if err := r.AfterPublish(ev); err != nil {
			return fmt.Errorf("after publish %s: %w", ev.ID, err)
		}
	}
	_, err = r.DB.ExecContext(ctx, `UPDATE outbox SET sent_at = ? WHERE seq = ?`,
		time.Now().UTC().Format(time.RFC3339Nano), seq)
	if err != nil {
		return fmt.Errorf("mark row %d sent: %w", seq, err)
	}
	return nil
}

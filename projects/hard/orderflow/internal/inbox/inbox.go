// Package inbox is the receiving half of reliable messaging.
//
// JetStream redelivers a message until it is acked, and the outbox may
// publish an event twice, so every consumer sees duplicates. The inbox table
// (keyed by event ID, in the service's own database) turns at-least-once
// into effectively-once:
//
//	BEGIN IMMEDIATE
//	  INSERT INTO inbox (event_id)      -- conflict → duplicate → skip
//	  handler(tx)                       -- business rows + outbox rows
//	COMMIT
//	ack                                 -- only after the commit
//
// Crash before the commit: nothing was recorded, redelivery re-runs the
// handler. Crash between commit and ack: redelivery hits the inbox row and
// is acked without running anything. A handler that keeps failing is
// retried with exponential backoff, and on its last allowed delivery the
// message goes to dlq.<stream> together with the reason, so one poison
// message cannot block the stream forever.
package inbox

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"orderflow/internal/event"
	"orderflow/internal/sqlite"
)

const Schema = `
CREATE TABLE IF NOT EXISTS inbox (
	event_id    TEXT PRIMARY KEY,
	received_at TEXT NOT NULL
) WITHOUT ROWID;`

// Handler runs inside the inbox transaction. Returning an error rolls
// everything back, including the inbox row, and the message is retried.
type Handler func(ctx context.Context, tx *sql.Tx, ev event.Event) error

type Consumer struct {
	DB         *sql.DB
	JS         jetstream.JetStream
	Stream     string // also the durable consumer's name
	Handle     Handler
	MaxDeliver int           // deliveries before the message is dead-lettered
	AckWait    time.Duration // how long JetStream waits for an ack before redelivering
	RetryBase  time.Duration // first retry delay; doubles per delivery

	// Fault-injection hooks. An error from BeforeCommit rolls the transaction
	// back; an error from BeforeAck leaves a committed message unacked —
	// a crash between commit and ack.
	BeforeCommit func(event.Event) error
	BeforeAck    func(event.Event) error

	Processed, Duplicates, DeadLettered atomic.Int64
}

func NewConsumer(db *sql.DB, js jetstream.JetStream, stream string, h Handler) *Consumer {
	return &Consumer{
		DB: db, JS: js, Stream: stream, Handle: h,
		MaxDeliver: 5, AckWait: 5 * time.Second, RetryBase: 100 * time.Millisecond,
	}
}

// Run blocks until ctx is done. Handlers run one at a time, in delivery order.
func (c *Consumer) Run(ctx context.Context) error {
	cons, err := c.JS.CreateOrUpdateConsumer(ctx, c.Stream, jetstream.ConsumerConfig{
		Durable:    c.Stream,
		AckPolicy:  jetstream.AckExplicitPolicy,
		MaxDeliver: c.MaxDeliver,
		AckWait:    c.AckWait,
	})
	if err != nil {
		return fmt.Errorf("create consumer on %s: %w", c.Stream, err)
	}
	// A small prefetch: the AckWait clock starts when the server hands a
	// message over, not when we get to it.
	cc, err := cons.Consume(func(m jetstream.Msg) { c.process(ctx, m) }, jetstream.PullMaxMessages(8))
	if err != nil {
		return fmt.Errorf("consume %s: %w", c.Stream, err)
	}
	<-ctx.Done()
	cc.Drain()
	<-cc.Closed()
	return nil
}

func (c *Consumer) process(ctx context.Context, msg jetstream.Msg) {
	ev, err := c.apply(ctx, msg.Data())
	switch {
	case ctx.Err() != nil:
		return // shutting down: leave it unacked, JetStream redelivers
	case err != nil:
		c.fail(ctx, msg, err)
	case c.BeforeAck != nil && c.BeforeAck(ev) != nil:
		return
	default:
		slog.Debug("acking", "stream", c.Stream, "type", ev.Type, "event", ev.ID, "correlation", ev.CorrelationID)
		_ = msg.Ack() // a lost ack is just another redelivery, and the inbox absorbs it
	}
}

func (c *Consumer) apply(ctx context.Context, data []byte) (event.Event, error) {
	ev, err := event.Decode(data)
	if err != nil {
		return ev, err
	}
	duplicate := false
	err = sqlite.InTx(ctx, c.DB, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			`INSERT INTO inbox (event_id, received_at) VALUES (?, ?) ON CONFLICT DO NOTHING`,
			ev.ID, time.Now().UTC().Format(time.RFC3339Nano))
		if err != nil {
			return fmt.Errorf("inbox insert: %w", err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			duplicate = true
			return nil
		}
		if err := c.Handle(ctx, tx, ev); err != nil {
			return fmt.Errorf("handle %s: %w", ev.Type, err)
		}
		if c.BeforeCommit != nil {
			return c.BeforeCommit(ev)
		}
		return nil
	})
	if err != nil {
		return ev, err
	}
	if duplicate {
		c.Duplicates.Add(1)
	} else {
		c.Processed.Add(1)
	}
	return ev, nil
}

func (c *Consumer) fail(ctx context.Context, msg jetstream.Msg, cause error) {
	meta, err := msg.Metadata()
	if err != nil {
		return // not a JetStream message; nothing sensible to do
	}
	if int(meta.NumDelivered) < c.MaxDeliver {
		slog.Warn("handler failed, retrying", "stream", c.Stream, "delivery", meta.NumDelivered, "err", cause)
		delay := min(c.RetryBase<<(meta.NumDelivered-1), 5*time.Second)
		_ = msg.NakWithDelay(delay) // a lost NAK just means AckWait redelivers
		return
	}

	dead := nats.NewMsg("dlq." + c.Stream)
	dead.Data = msg.Data()
	dead.Header.Set("Failure-Reason", cause.Error())
	dead.Header.Set("Original-Subject", msg.Subject())
	dead.Header.Set("Deliveries", strconv.FormatUint(meta.NumDelivered, 10))
	// Keyed by stream sequence so a retried dead-lettering cannot double up.
	dead.Header.Set(nats.MsgIdHdr, fmt.Sprintf("%s-%d", c.Stream, meta.Sequence.Stream))
	if _, err := c.JS.PublishMsg(ctx, dead); err != nil {
		// Still in the stream, just not acked: visible and replayable.
		slog.Error("dead-letter publish failed", "stream", c.Stream, "err", err)
		return
	}
	c.DeadLettered.Add(1)
	slog.Error("dead-lettered", "stream", c.Stream, "deliveries", meta.NumDelivered, "err", cause)
	_ = msg.Ack()
}

package inbox_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"orderflow/internal/bus"
	"orderflow/internal/event"
	"orderflow/internal/inbox"
	"orderflow/internal/sqlite"
)

const stream = "inventory" // any stream works; the consumer does not care

// rig is one consumer on a real embedded broker whose handler's only effect
// is a row in a table with no unique constraint, so a double-processed event
// shows up as two rows.
type rig struct {
	bus      *bus.Bus
	db       *sql.DB
	consumer *inbox.Consumer
	attempts atomic.Int64
	handle   func(ctx context.Context, tx *sql.Tx, ev event.Event) error
}

func newRig(t *testing.T) *rig {
	t.Helper()
	b, err := bus.Start(t.Context(), filepath.Join(t.TempDir(), "nats"))
	require.NoError(t, err)
	t.Cleanup(b.Close)
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "svc.db"))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	_, err = db.Exec(inbox.Schema + `CREATE TABLE effects (event_id TEXT NOT NULL)`)
	require.NoError(t, err)

	r := &rig{bus: b, db: db}
	r.handle = func(ctx context.Context, tx *sql.Tx, ev event.Event) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO effects (event_id) VALUES (?)`, ev.ID)
		return err
	}
	r.consumer = inbox.NewConsumer(db, b.JS, stream, func(ctx context.Context, tx *sql.Tx, ev event.Event) error {
		r.attempts.Add(1)
		return r.handle(ctx, tx, ev)
	})
	r.consumer.AckWait = 300 * time.Millisecond
	r.consumer.RetryBase = 10 * time.Millisecond
	return r
}

func (r *rig) start(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Go(func() { require.NoError(t, r.consumer.Run(ctx)) })
	t.Cleanup(func() { cancel(); wg.Wait() })
}

// publish sends without a Nats-Msg-Id, so the broker never dedupes: only the
// inbox stands between a duplicate and the handler.
func (r *rig) publish(t *testing.T, ev event.Event, times int) {
	t.Helper()
	b, err := jsonBytes(ev)
	require.NoError(t, err)
	for range times {
		_, err := r.bus.JS.Publish(t.Context(), ev.Type, b)
		require.NoError(t, err)
	}
}

func (r *rig) count(t *testing.T, query string) int {
	t.Helper()
	var n int
	require.NoError(t, r.db.QueryRow(query).Scan(&n))
	return n
}

func newEvent(t *testing.T, typ string) event.Event {
	t.Helper()
	ev, err := event.New(typ, "ord_1", "cor_1", event.Cmd{OrderID: "ord_1"})
	require.NoError(t, err)
	return ev
}

func TestDuplicatesRunTheHandlerOnce(t *testing.T) {
	r := newRig(t)
	r.start(t)
	r.publish(t, newEvent(t, "inventory.reserve"), 3)

	require.Eventually(t, func() bool { return r.consumer.Duplicates.Load() == 2 }, 30*time.Second, 10*time.Millisecond)
	require.Equal(t, 1, r.count(t, `SELECT COUNT(*) FROM effects`))
	require.Equal(t, 1, r.count(t, `SELECT COUNT(*) FROM inbox`))
	require.EqualValues(t, 1, r.consumer.Processed.Load())
}

func TestFailedHandlerRollsBackEverythingAndIsRetried(t *testing.T) {
	r := newRig(t)
	failed := false
	base := r.handle
	r.handle = func(ctx context.Context, tx *sql.Tx, ev event.Event) error {
		if err := base(ctx, tx, ev); err != nil {
			return err
		}
		if !failed {
			failed = true
			return errors.New("crash after writing the effect")
		}
		return nil
	}
	r.start(t)
	r.publish(t, newEvent(t, "inventory.reserve"), 1)

	require.Eventually(t, func() bool { return r.consumer.Processed.Load() == 1 }, 30*time.Second, 10*time.Millisecond)
	require.Equal(t, 1, r.count(t, `SELECT COUNT(*) FROM effects`), "the first attempt's write must have been rolled back")
	require.Equal(t, 1, r.count(t, `SELECT COUNT(*) FROM inbox`))
	require.EqualValues(t, 2, r.attempts.Load())
}

func TestCrashBetweenCommitAndAckRedeliversAndDedupes(t *testing.T) {
	r := newRig(t)
	crashed := false
	r.consumer.BeforeAck = func(event.Event) error {
		if !crashed {
			crashed = true
			return errors.New("crash before ack")
		}
		return nil
	}
	r.start(t)
	r.publish(t, newEvent(t, "inventory.reserve"), 1)

	require.Eventually(t, func() bool { return r.consumer.Duplicates.Load() == 1 }, 30*time.Second, 10*time.Millisecond)
	require.Equal(t, 1, r.count(t, `SELECT COUNT(*) FROM effects`))
	require.EqualValues(t, 1, r.attempts.Load(), "the redelivery must not reach the handler")
}

func TestPoisonMessageIsDeadLetteredAndTheStreamKeepsFlowing(t *testing.T) {
	r := newRig(t)
	r.consumer.MaxDeliver = 3
	base := r.handle
	r.handle = func(ctx context.Context, tx *sql.Tx, ev event.Event) error {
		if ev.Type == "inventory.poison" {
			return errors.New("cannot be processed")
		}
		return base(ctx, tx, ev)
	}
	r.start(t)
	poison := newEvent(t, "inventory.poison")
	r.publish(t, poison, 1)
	good := newEvent(t, "inventory.reserve")
	r.publish(t, good, 1)

	require.Eventually(t, func() bool { return r.consumer.DeadLettered.Load() == 1 && r.consumer.Processed.Load() == 1 },
		30*time.Second, 10*time.Millisecond)
	require.EqualValues(t, 3+1, r.attempts.Load(), "three deliveries of the poison message plus the good one")
	require.Equal(t, 1, r.count(t, `SELECT COUNT(*) FROM effects`))
	require.Equal(t, 1, r.count(t, `SELECT COUNT(*) FROM inbox`), "a dead-lettered message leaves no inbox row")

	dlq, err := r.bus.JS.Stream(t.Context(), "dlq")
	require.NoError(t, err)
	msg, err := dlq.GetMsg(t.Context(), 1)
	require.NoError(t, err)
	require.Equal(t, "dlq.inventory", msg.Subject)
	require.Equal(t, "inventory.poison", msg.Header.Get("Original-Subject"))
	require.Equal(t, "3", msg.Header.Get("Deliveries"))
	require.Contains(t, msg.Header.Get("Failure-Reason"), "cannot be processed")
	got, err := event.Decode(msg.Data)
	require.NoError(t, err)
	require.Equal(t, poison.ID, got.ID, "the original payload travels with the reason")

	info, err := dlq.Info(t.Context())
	require.NoError(t, err)
	require.EqualValues(t, 1, info.State.Msgs)
}

func jsonBytes(ev event.Event) ([]byte, error) { return json.Marshal(ev) }

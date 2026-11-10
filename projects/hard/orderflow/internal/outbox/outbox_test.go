package outbox

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"

	"orderflow/internal/event"
	"orderflow/internal/sqlite"
)

type fakePub struct {
	mu     sync.Mutex
	sent   []*nats.Msg
	failOn int // fail the Nth call (1-based) once; 0 never fails
	calls  int
}

func (f *fakePub) PublishMsg(_ context.Context, m *nats.Msg, _ ...jetstream.PublishOpt) (*jetstream.PubAck, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.calls == f.failOn {
		return nil, errors.New("broker down")
	}
	f.sent = append(f.sent, m)
	return &jetstream.PubAck{}, nil
}

func newDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "outbox.db"))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	_, err = db.Exec(Schema)
	require.NoError(t, err)
	return db
}

func insert(t *testing.T, db *sql.DB, types ...string) []string {
	t.Helper()
	var ids []string
	require.NoError(t, sqlite.InTx(t.Context(), db, func(tx *sql.Tx) error {
		for _, typ := range types {
			ev, err := event.New(typ, "ord_1", "cor_1", map[string]int{"n": 1})
			if err != nil {
				return err
			}
			ids = append(ids, ev.ID)
			if err := Insert(t.Context(), tx, ev); err != nil {
				return err
			}
		}
		return nil
	}))
	return ids
}

func unsent(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM outbox WHERE sent_at IS NULL`).Scan(&n))
	return n
}

func TestInsertRollsBackWithTheBusinessTransaction(t *testing.T) {
	db := newDB(t)
	boom := errors.New("business rule failed")
	err := sqlite.InTx(t.Context(), db, func(tx *sql.Tx) error {
		ev, _ := event.New("inventory.reserve", "ord_1", "cor_1", nil)
		require.NoError(t, Insert(t.Context(), tx, ev))
		return boom
	})
	require.ErrorIs(t, err, boom)
	require.Zero(t, unsent(t, db), "an event must not outlive the transaction that produced it")
}

func TestFlushPublishesInOrderWithMessageID(t *testing.T) {
	db := newDB(t)
	ids := insert(t, db, "inventory.reserve", "payments.charge", "inventory.commit")
	pub := &fakePub{}
	r := NewRelay(db, pub)

	require.NoError(t, r.Flush(t.Context()))
	require.Len(t, pub.sent, 3)
	for i, m := range pub.sent {
		require.Equal(t, ids[i], m.Header.Get(nats.MsgIdHdr))
	}
	require.Equal(t, "inventory.reserve", pub.sent[0].Subject)
	require.Equal(t, "payments.charge", pub.sent[1].Subject)
	require.Zero(t, unsent(t, db))

	require.NoError(t, r.Flush(t.Context()))
	require.Len(t, pub.sent, 3, "sent rows are not published again")
}

func TestFlushStopsAtFirstFailureAndKeepsOrder(t *testing.T) {
	db := newDB(t)
	ids := insert(t, db, "inventory.reserve", "payments.charge", "inventory.commit")
	pub := &fakePub{failOn: 2}
	r := NewRelay(db, pub)

	require.Error(t, r.Flush(t.Context()))
	require.Len(t, pub.sent, 1, "row 3 must not overtake the failed row 2")
	require.Equal(t, 2, unsent(t, db))

	require.NoError(t, r.Flush(t.Context()))
	require.Len(t, pub.sent, 3)
	require.Equal(t, ids[1], pub.sent[1].Header.Get(nats.MsgIdHdr))
	require.Equal(t, ids[2], pub.sent[2].Header.Get(nats.MsgIdHdr))
}

func TestCrashAfterPublishRepublishesTheSameEvent(t *testing.T) {
	db := newDB(t)
	ids := insert(t, db, "inventory.reserve")
	pub := &fakePub{}
	r := NewRelay(db, pub)
	crashed := false
	r.AfterPublish = func(event.Event) error {
		if !crashed {
			crashed = true
			return errors.New("crash")
		}
		return nil
	}

	require.Error(t, r.Flush(t.Context()))
	require.Equal(t, 1, unsent(t, db), "the row stays unsent: the relay cannot know the publish succeeded")
	require.NoError(t, r.Flush(t.Context()))
	require.Len(t, pub.sent, 2, "at-least-once: the event went out twice")
	require.Equal(t, ids[0], pub.sent[0].Header.Get(nats.MsgIdHdr))
	require.Equal(t, ids[0], pub.sent[1].Header.Get(nats.MsgIdHdr))
	require.Zero(t, unsent(t, db))
}

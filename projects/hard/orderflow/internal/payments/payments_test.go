package payments

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"orderflow/internal/event"
	"orderflow/internal/sqlite"
)

func charge(t *testing.T, s *Service, orderID string, amount int) (replyType string) {
	t.Helper()
	ev, err := event.New(event.Charge, orderID, "cor_1", event.Cmd{OrderID: orderID, Amount: amount})
	require.NoError(t, err)
	require.NoError(t, sqlite.InTx(context.Background(), s.DB, func(tx *sql.Tx) error { return s.handle(context.Background(), tx, ev) }))
	require.NoError(t, s.DB.QueryRow(`SELECT json_extract(payload, '$.type') FROM outbox ORDER BY seq DESC LIMIT 1`).Scan(&replyType))
	return replyType
}

func newService(t *testing.T) *Service {
	t.Helper()
	s, err := New(filepath.Join(t.TempDir(), "payments.db"), nil)
	require.NoError(t, err)
	t.Cleanup(func() { s.Close() })
	return s
}

func TestChargeAndDecline(t *testing.T) {
	s := newService(t)
	require.Equal(t, event.PaymentSucceeded, charge(t, s, "o1", 100), "no Decline hook never declines")

	s.Decline = func(c event.Cmd) bool { return c.Amount > 500 }
	require.Equal(t, event.PaymentSucceeded, charge(t, s, "o2", 500))
	require.Equal(t, event.PaymentDeclined, charge(t, s, "o3", 501))

	rows, err := s.DB.Query(`SELECT order_id, status FROM payments ORDER BY id`)
	require.NoError(t, err)
	defer rows.Close()
	var got []string
	for rows.Next() {
		var id, status string
		require.NoError(t, rows.Scan(&id, &status))
		got = append(got, id+":"+status)
	}
	require.Equal(t, []string{"o1:CHARGED", "o2:CHARGED", "o3:DECLINED"}, got)
}

func TestRandomDeclineRate(t *testing.T) {
	never, always := RandomDecline(0), RandomDecline(1)
	declined := 0
	for range 1000 {
		require.False(t, never(event.Cmd{}))
		require.True(t, always(event.Cmd{}))
		if RandomDecline(0.5)(event.Cmd{}) {
			declined++
		}
	}
	require.InDelta(t, 500, declined, 100)
}

func TestUnexpectedEventTypeIsAnError(t *testing.T) {
	s := newService(t)
	ev, err := event.New("payments.refund", "o1", "cor_1", event.Cmd{OrderID: "o1"})
	require.NoError(t, err)
	err = sqlite.InTx(context.Background(), s.DB, func(tx *sql.Tx) error { return s.handle(context.Background(), tx, ev) })
	require.ErrorContains(t, err, "unexpected event type")
}

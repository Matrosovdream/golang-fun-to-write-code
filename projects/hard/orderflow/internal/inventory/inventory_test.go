package inventory

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"orderflow/internal/event"
	"orderflow/internal/sqlite"
)

func newService(t *testing.T) *Service {
	t.Helper()
	s, err := New(filepath.Join(t.TempDir(), "inventory.db"), nil, map[string]int{"widget": 5})
	require.NoError(t, err)
	t.Cleanup(func() { s.Close() })
	return s
}

func send(s *Service, typ, orderID, sku string, qty int) error {
	cmd := event.Cmd{OrderID: orderID, SKU: sku, Qty: qty}
	ev, err := event.New(typ, orderID, "cor_1", cmd)
	if err != nil {
		return err
	}
	return sqlite.InTx(context.Background(), s.DB, func(tx *sql.Tx) error { return s.handle(context.Background(), tx, ev) })
}

func replies(t *testing.T, s *Service) []string {
	t.Helper()
	rows, err := s.DB.Query(`SELECT json_extract(payload, '$.type') FROM outbox ORDER BY seq`)
	require.NoError(t, err)
	defer rows.Close()
	var out []string
	for rows.Next() {
		var typ string
		require.NoError(t, rows.Scan(&typ))
		out = append(out, typ)
	}
	return out
}

func widget(t *testing.T, s *Service) Stock {
	t.Helper()
	all, err := s.Stock(t.Context())
	require.NoError(t, err)
	require.Len(t, all, 1)
	return all[0]
}

func TestReserveCommitRelease(t *testing.T) {
	s := newService(t)
	require.NoError(t, send(s, event.Reserve, "o1", "widget", 2))
	require.NoError(t, send(s, event.Reserve, "o2", "widget", 3))
	require.Equal(t, Stock{"widget", 5, 0, 5, 0}, widget(t, s))

	require.NoError(t, send(s, event.Commit, "o1", "widget", 2))
	require.NoError(t, send(s, event.Release, "o2", "widget", 3))
	require.Equal(t, Stock{"widget", 5, 3, 0, 2}, widget(t, s))
	require.Equal(t, []string{event.StockReserved, event.StockReserved, event.StockCommitted, event.StockReleased}, replies(t, s))
}

func TestRunningOutIsARejectionNotAnError(t *testing.T) {
	s := newService(t)
	for name, c := range map[string]struct {
		sku string
		qty int
	}{"too many": {"widget", 6}, "unknown sku": {"gadget", 1}, "zero": {"widget", 0}, "negative": {"widget", -3}} {
		require.NoError(t, send(s, event.Reserve, "o_"+name, c.sku, c.qty), name)
	}
	require.Equal(t, Stock{"widget", 5, 5, 0, 0}, widget(t, s))
	require.Equal(t, []string{event.StockRejected, event.StockRejected, event.StockRejected, event.StockRejected}, replies(t, s))
}

func TestSettlingTwiceOrWithoutReservingFailsAndChangesNothing(t *testing.T) {
	s := newService(t)
	require.NoError(t, send(s, event.Reserve, "o1", "widget", 2))
	require.NoError(t, send(s, event.Release, "o1", "widget", 2))
	before := widget(t, s)

	require.Error(t, send(s, event.Release, "o1", "widget", 2), "double release would hand out the units twice")
	require.Error(t, send(s, event.Commit, "o1", "widget", 2), "commit after release")
	require.Error(t, send(s, event.Commit, "never-reserved", "widget", 2))
	require.Equal(t, before, widget(t, s))
	require.Len(t, replies(t, s), 2)
}

func TestFailedHandlerLeavesNoPartialReservation(t *testing.T) {
	s := newService(t)
	require.NoError(t, send(s, event.Reserve, "o1", "widget", 2))
	// A second reserve for the same order violates the reservation key after
	// the stock UPDATE already ran: the whole transaction must roll back.
	require.Error(t, send(s, event.Reserve, "o1", "widget", 2))
	require.Equal(t, Stock{"widget", 5, 3, 2, 0}, widget(t, s))
}

func TestSeedingKeepsExistingCounts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "inventory.db")
	s, err := New(path, nil, map[string]int{"widget": 5})
	require.NoError(t, err)
	require.NoError(t, send(s, event.Reserve, "o1", "widget", 2))
	require.NoError(t, s.Close())

	s, err = New(path, nil, map[string]int{"widget": 99})
	require.NoError(t, err)
	defer s.Close()
	require.Equal(t, Stock{"widget", 5, 3, 2, 0}, widget(t, s))
}

func TestStockHandler(t *testing.T) {
	s := newService(t)
	rec := httptest.NewRecorder()
	s.StockHandler().ServeHTTP(rec, httptest.NewRequest("GET", "/stock", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `[{"sku":"widget","initial":5,"available":5,"reserved":0,"sold":0}]`, rec.Body.String())
}

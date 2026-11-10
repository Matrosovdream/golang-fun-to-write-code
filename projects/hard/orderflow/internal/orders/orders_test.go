package orders

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"orderflow/internal/event"
	"orderflow/internal/sqlite"
)

var allStates = []State{Pending, StockReserved, Paid, Confirmed, Cancelling, Cancelled}

// The legal edges, written out a second time on purpose: the test compares
// the transitions table against this list instead of against itself.
var legalEdges = map[[2]State]bool{
	{Pending, StockReserved}:    true,
	{Pending, Cancelled}:        true,
	{StockReserved, Paid}:       true,
	{StockReserved, Cancelling}: true,
	{Paid, Confirmed}:           true,
	{Cancelling, Cancelled}:     true,
}

func TestTransitionsEnumerated(t *testing.T) {
	for _, from := range allStates {
		for _, to := range allStates {
			err := Transition(from, to)
			if legalEdges[[2]State{from, to}] {
				require.NoError(t, err, "%s -> %s", from, to)
			} else {
				require.ErrorIs(t, err, ErrIllegalTransition, "%s -> %s", from, to)
			}
		}
	}
	require.True(t, Confirmed.Terminal())
	require.True(t, Cancelled.Terminal())
	for _, s := range []State{Pending, StockReserved, Paid, Cancelling} {
		require.False(t, s.Terminal(), s)
	}
}

func newService(t *testing.T) *Service {
	t.Helper()
	s, err := New(filepath.Join(t.TempDir(), "orders.db"), nil)
	require.NoError(t, err)
	t.Cleanup(func() { s.Close() })
	return s
}

func create(t *testing.T, s *Service) Order {
	t.Helper()
	o, err := s.Create(t.Context(), CreateRequest{SKU: "widget", Qty: 2, Amount: 200}, "cor_1")
	require.NoError(t, err)
	return o
}

func deliver(s *Service, typ, orderID, reason string) error {
	ev, err := event.New(typ, orderID, "cor_1", event.Reply{OrderID: orderID, Reason: reason})
	if err != nil {
		return err
	}
	return sqlite.InTx(context.Background(), s.DB, func(tx *sql.Tx) error { return s.handle(context.Background(), tx, ev) })
}

func outboxTypes(t *testing.T, s *Service) []string {
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

// Every (state, reply) pair: the reply is applied exactly when the state
// machine allows the move, and an illegal one changes and emits nothing.
func TestEveryReplyInEveryState(t *testing.T) {
	s := newService(t)
	for _, from := range allStates {
		for typ, r := range reactions {
			t.Run(string(from)+"/"+typ, func(t *testing.T) {
				o := create(t, s)
				_, err := s.DB.Exec(`UPDATE orders SET state = ? WHERE id = ?`, from, o.ID)
				require.NoError(t, err)
				before := len(outboxTypes(t, s))

				err = deliver(s, typ, o.ID, "")
				got, gerr := s.Get(t.Context(), o.ID)
				require.NoError(t, gerr)
				if legalEdges[[2]State{from, r.to}] {
					require.NoError(t, err)
					require.Equal(t, r.to, got.State)
					wantCmds := 0
					if r.next != "" {
						wantCmds = 1
					}
					require.Len(t, outboxTypes(t, s), before+wantCmds)
				} else {
					require.ErrorIs(t, err, ErrIllegalTransition)
					require.Equal(t, from, got.State)
					require.Len(t, outboxTypes(t, s), before, "an illegal reply must not emit a command")
				}
			})
		}
	}
}

func TestSagaOrchestration(t *testing.T) {
	tests := []struct {
		name     string
		replies  []string
		want     []State
		commands []string
	}{
		{"happy path",
			[]string{event.StockReserved, event.PaymentSucceeded, event.StockCommitted},
			[]State{Pending, StockReserved, Paid, Confirmed},
			[]string{event.Reserve, event.Charge, event.Commit}},
		{"payment declined compensates",
			[]string{event.StockReserved, event.PaymentDeclined, event.StockReleased},
			[]State{Pending, StockReserved, Cancelling, Cancelled},
			[]string{event.Reserve, event.Charge, event.Release}},
		{"out of stock needs no compensation",
			[]string{event.StockRejected},
			[]State{Pending, Cancelled},
			[]string{event.Reserve}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newService(t)
			o := create(t, s)
			for _, typ := range tc.replies {
				require.NoError(t, deliver(s, typ, o.ID, ""))
			}
			got, err := s.Get(t.Context(), o.ID)
			require.NoError(t, err)
			require.Equal(t, tc.want, got.History)
			require.Equal(t, tc.commands, outboxTypes(t, s))
		})
	}
}

func TestReasonSurvivesCompensation(t *testing.T) {
	s := newService(t)
	o := create(t, s)
	for _, step := range []struct{ typ, reason string }{
		{event.StockReserved, ""}, {event.PaymentDeclined, "card declined"}, {event.StockReleased, ""},
	} {
		require.NoError(t, deliver(s, step.typ, o.ID, step.reason))
	}
	got, err := s.Get(t.Context(), o.ID)
	require.NoError(t, err)
	require.Equal(t, "card declined", got.Reason)
}

func TestUnknownOrderAndEventAreErrors(t *testing.T) {
	s := newService(t)
	require.ErrorIs(t, deliver(s, event.StockReserved, "ord_missing", ""), ErrNotFound)
	o := create(t, s)
	require.ErrorContains(t, deliver(s, "orders.bogus", o.ID, ""), "unexpected event type")
}

func TestCreateValidatesAndWritesOrderAndCommandTogether(t *testing.T) {
	s := newService(t)
	for _, req := range []CreateRequest{{}, {SKU: "w", Qty: 0, Amount: 1}, {SKU: "w", Qty: 1, Amount: -5}} {
		_, err := s.Create(t.Context(), req, "")
		require.ErrorIs(t, err, ErrInvalid)
	}
	require.Empty(t, outboxTypes(t, s), "an invalid order must leave nothing behind")

	o := create(t, s)
	require.Equal(t, []string{event.Reserve}, outboxTypes(t, s))
	var corr string
	require.NoError(t, s.DB.QueryRow(`SELECT json_extract(payload, '$.correlation_id') FROM outbox`).Scan(&corr))
	require.Equal(t, "cor_1", corr)
	got, err := s.Get(t.Context(), o.ID)
	require.NoError(t, err)
	require.Equal(t, []State{Pending}, got.History)
}

func TestREST(t *testing.T) {
	s := newService(t)
	mux := http.NewServeMux()
	s.Routes(mux)
	do := func(method, path, body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(method, path, bytes.NewBufferString(body)))
		return rec
	}

	rec := do("POST", "/orders", `{"sku":"widget","qty":2,"amount":200}`)
	require.Equal(t, http.StatusAccepted, rec.Code)
	var o Order
	require.NoError(t, jsonDecode(rec.Body.Bytes(), &o))
	require.Equal(t, Pending, o.State)
	require.Equal(t, "/orders/"+o.ID, rec.Header().Get("Location"))

	require.Equal(t, http.StatusBadRequest, do("POST", "/orders", `{"sku":"widget","qty":0,"amount":200}`).Code)
	require.Equal(t, http.StatusBadRequest, do("POST", "/orders", `{"sku":"w","qty":1,"amount":1,"extra":true}`).Code)
	require.Equal(t, http.StatusBadRequest, do("POST", "/orders", `not json`).Code)

	rec = do("GET", "/orders/"+o.ID, "")
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `"history":["PENDING"]`)
	require.Equal(t, http.StatusNotFound, do("GET", "/orders/ord_nope", "").Code)

	rec = do("GET", "/orders", "")
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), o.ID)
}

func jsonDecode(b []byte, v any) error { return json.Unmarshal(b, v) }

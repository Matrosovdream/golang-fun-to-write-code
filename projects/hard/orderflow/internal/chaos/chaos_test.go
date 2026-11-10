package chaos

import (
	"encoding/json"
	"math/rand/v2"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"orderflow/internal/event"
	"orderflow/internal/orders"
)

func TestHappyPath(t *testing.T) {
	s := newSystem(t, map[string]int{"widget": 10})
	// Created before anything runs: the order and its first command wait in
	// the outbox until the relay starts.
	id := s.place(t, "widget", 3)
	s.start(t)
	s.await(t, id)

	o := s.order(t, id)
	require.Equal(t, orders.Confirmed, o.State)
	require.Equal(t, []orders.State{orders.Pending, orders.StockReserved, orders.Paid, orders.Confirmed}, o.History)
	st := s.stock(t, "widget")
	require.Equal(t, 7, st.Available)
	require.Equal(t, 3, st.Sold)
	require.Zero(t, st.Reserved)
	s.requireInvariants(t)
}

func TestOutOfStockCancelsWithoutCompensation(t *testing.T) {
	s := newSystem(t, map[string]int{"widget": 2})
	s.start(t)
	id := s.place(t, "widget", 3)
	s.await(t, id)

	o := s.order(t, id)
	require.Equal(t, []orders.State{orders.Pending, orders.Cancelled}, o.History)
	require.Contains(t, o.Reason, "insufficient stock")
	require.Equal(t, 2, s.stock(t, "widget").Available)
	require.Zero(t, s.count(t, s.pay.DB, `SELECT COUNT(*) FROM payments`), "nothing to charge for a rejected order")
}

func TestDeclinedPaymentReleasesTheStock(t *testing.T) {
	s := newSystem(t, map[string]int{"widget": 10})
	s.pay.Decline = func(c event.Cmd) bool { return c.Amount > 200 }
	s.start(t)
	declined := s.place(t, "widget", 3)
	accepted := s.place(t, "widget", 2)
	s.await(t, declined, accepted)

	o := s.order(t, declined)
	require.Equal(t, []orders.State{orders.Pending, orders.StockReserved, orders.Cancelling, orders.Cancelled}, o.History)
	require.Equal(t, "card declined", o.Reason)
	require.Equal(t, orders.Confirmed, s.order(t, accepted).State)

	st := s.stock(t, "widget")
	require.Equal(t, 8, st.Available, "the declined order's 3 units are back")
	require.Equal(t, 2, st.Sold)
	require.Zero(t, st.Reserved)
	require.Equal(t, 1, s.count(t, s.inv.DB, `SELECT COUNT(*) FROM reservations WHERE state = 'RELEASED'`))
	s.requireInvariants(t)
}

// Every event suffers one fault. In each case the final state is identical
// and every event took effect exactly once: 6 events per confirmed order.
func TestFaultsCauseNoDuplicateEffects(t *testing.T) {
	const orderCount, eventsPerOrder = 3, 6
	tests := []struct {
		name           string
		inject         func(s *system)
		wantDuplicates int64 // deliveries that must be recognised as duplicates, one per event
	}{
		{"relay crashes after publish, before marking sent", func(s *system) {
			s.disableBrokerDedupe()
			var o once
			for _, n := range s.nodes() {
				n.Relay.AfterPublish = func(ev event.Event) error {
					if o.first(ev.ID) {
						return errInjected
					}
					return nil
				}
			}
		}, orderCount * eventsPerOrder},
		{"consumer crashes after commit, before ack", func(s *system) {
			var o once
			for _, n := range s.nodes() {
				n.Consumer.BeforeAck = func(ev event.Event) error {
					if o.first(ev.ID) {
						return errInjected
					}
					return nil
				}
			}
		}, orderCount * eventsPerOrder},
		{"consumer crashes before commit", func(s *system) {
			var o once
			for _, n := range s.nodes() {
				n.Consumer.BeforeCommit = func(ev event.Event) error {
					if o.first(ev.ID) {
						return errInjected
					}
					return nil
				}
			}
		}, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := newSystem(t, map[string]int{"widget": 10})
			tc.inject(s)
			s.start(t)
			var ids []string
			for range orderCount {
				ids = append(ids, s.place(t, "widget", 1))
			}
			s.await(t, ids...)
			s.awaitDuplicates(t, tc.wantDuplicates) // the late copies must not change anything

			for _, id := range ids {
				require.Equal(t, orders.Confirmed, s.order(t, id).State)
				require.Equal(t, 1, s.count(t, s.pay.DB, `SELECT COUNT(*) FROM payments WHERE order_id = ?`, id))
			}
			st := s.stock(t, "widget")
			require.Equal(t, 10-orderCount, st.Available)
			require.Equal(t, orderCount, st.Sold)
			require.EqualValues(t, orderCount*eventsPerOrder, s.processed())
			require.Zero(t, s.deadLetters())
			if tc.wantDuplicates == 0 {
				require.Zero(t, s.duplicates())
			}
			s.requireInvariants(t)
		})
	}
}

// With Nats-Msg-Id intact JetStream absorbs the relay's republish itself.
// Same crash as above, but the consumers never see a duplicate. The stream
// still holds one copy per event; correctness just does not depend on it.
func TestBrokerDedupeAbsorbsRelayRepublish(t *testing.T) {
	s := newSystem(t, map[string]int{"widget": 10})
	var o once
	for _, n := range s.nodes() {
		n.Relay.AfterPublish = func(ev event.Event) error {
			if o.first(ev.ID) {
				return errInjected
			}
			return nil
		}
	}
	s.start(t)
	id := s.place(t, "widget", 1)
	s.await(t, id)

	require.Equal(t, orders.Confirmed, s.order(t, id).State)
	require.Zero(t, s.duplicates())
	inv, err := s.bus.JS.Stream(t.Context(), "inventory")
	require.NoError(t, err)
	info, err := inv.Info(t.Context())
	require.NoError(t, err)
	require.EqualValues(t, 2, info.State.Msgs, "reserve and commit, once each")
}

func TestPoisonMessagesGoToTheDLQAndTheStreamKeepsFlowing(t *testing.T) {
	s := newSystem(t, map[string]int{"widget": 10})
	for _, n := range s.nodes() {
		n.Consumer.MaxDeliver = 3
	}
	valid, err := event.New("orders.stock_exploded", "ord_x", "cor_x", event.Reply{OrderID: "ord_x"})
	require.NoError(t, err)
	validBytes, err := json.Marshal(valid)
	require.NoError(t, err)
	badData, err := json.Marshal(event.Event{ID: "evt_bad", Type: event.Reserve, Data: json.RawMessage(`42`)})
	require.NoError(t, err)

	poison := []struct {
		subject, reason string
		data            []byte
	}{
		{"inventory.reserve", "decode envelope", []byte(`{nope`)},
		{"payments.charge", "missing id or type", []byte(`{"type":"payments.charge"}`)},
		{"orders.stock_exploded", "unexpected event type", validBytes},
		{"inventory.reserve", "decode inventory.reserve data", badData},
	}
	for _, p := range poison {
		_, err := s.bus.JS.Publish(t.Context(), p.subject, p.data)
		require.NoError(t, err)
	}
	// A healthy order queued behind (and between) the poison messages.
	id := s.place(t, "widget", 2)
	s.start(t)
	s.await(t, id)

	require.Equal(t, orders.Confirmed, s.order(t, id).State)
	require.Eventually(t, func() bool { return s.deadLetters() == int64(len(poison)) }, 90*time.Second, 20*time.Millisecond)

	dlq, err := s.bus.JS.Stream(t.Context(), "dlq")
	require.NoError(t, err)
	var reasons []string
	for seq := uint64(1); seq <= uint64(len(poison)); seq++ {
		m, err := dlq.GetMsg(t.Context(), seq)
		require.NoError(t, err)
		require.Equal(t, "3", m.Header.Get("Deliveries"))
		reasons = append(reasons, m.Header.Get("Original-Subject")+" | "+m.Header.Get("Failure-Reason"))
	}
	for _, p := range poison {
		require.True(t, containsAll(reasons, p.subject, p.reason), "no dead letter for %s / %s in %v", p.subject, p.reason, reasons)
	}
	s.requireInvariants(t)
}

func containsAll(lines []string, parts ...string) bool {
	for _, l := range lines {
		ok := true
		for _, p := range parts {
			ok = ok && strings.Contains(l, p)
		}
		if ok {
			return true
		}
	}
	return false
}

// faults is the seeded fault source shared by all the injected hooks.
type faults struct {
	mu  sync.Mutex
	rng *rand.Rand
}

func (f *faults) hit(p float64) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.rng.Float64() < p
}

func (f *faults) hook(p float64, n *atomic.Int64) func(event.Event) error {
	return func(event.Event) error {
		if f.hit(p) {
			n.Add(1)
			return errInjected
		}
		return nil
	}
}

// 100 orders placed concurrently against 90 units of stock, with relays
// crashing, consumers crashing before commit and before ack, and a quarter
// of the charges declined. Whatever the interleaving, the invariants hold.
// Rerun a failure with CHAOS_SEED=<seed>.
func TestChaosManyConcurrentOrders(t *testing.T) {
	seed := uint64(time.Now().UnixNano())
	if v := os.Getenv("CHAOS_SEED"); v != "" {
		var err error
		seed, err = strconv.ParseUint(v, 10, 64)
		require.NoError(t, err)
	}
	t.Logf("seed=%d (rerun with CHAOS_SEED=%d)", seed, seed)
	f := &faults{rng: rand.New(rand.NewPCG(seed, seed))}

	const initial = 90
	s := newSystem(t, map[string]int{"a": 40, "b": 30, "c": 20})
	s.disableBrokerDedupe()
	var relayCrashes, commitCrashes, ackCrashes atomic.Int64
	for _, n := range s.nodes() {
		n.Relay.AfterPublish = f.hook(0.15, &relayCrashes)
		n.Consumer.BeforeCommit = f.hook(0.05, &commitCrashes)
		n.Consumer.BeforeAck = f.hook(0.10, &ackCrashes)
	}
	s.pay.Decline = func(event.Cmd) bool { return f.hit(0.25) }
	s.start(t)

	skus := []string{"a", "b", "c"}
	reqs := make([]orders.CreateRequest, 100)
	for i := range reqs {
		qty := 1 + f.rng.IntN(3)
		reqs[i] = orders.CreateRequest{SKU: skus[f.rng.IntN(len(skus))], Qty: qty, Amount: 100 * qty}
	}
	ids := make([]string, len(reqs))
	var wg sync.WaitGroup
	for i, req := range reqs {
		wg.Go(func() {
			o, err := s.ord.Create(t.Context(), req, "")
			if assert.NoError(t, err) {
				ids[i] = o.ID
			}
		})
	}
	wg.Wait()
	require.NotContains(t, ids, "")
	s.await(t, ids...)

	// Every relay crash and every lost ack is one extra delivery of an event
	// that was already processed. Wait until all of them have been seen, so
	// the checks below run after the last duplicate.
	s.awaitDuplicates(t, relayCrashes.Load()+ackCrashes.Load())
	s.requireInvariants(t)
	require.Zero(t, s.deadLetters(), "no event may be dead-lettered by transient faults")

	var confirmed, outOfStock, declined, reserved int
	for _, id := range ids {
		o := s.order(t, id)
		if o.History[1] == orders.StockReserved {
			reserved++
		}
		switch {
		case o.State == orders.Confirmed:
			confirmed++
		case o.Reason == "card declined":
			declined++
		default:
			outOfStock++
		}
	}
	t.Logf("confirmed=%d declined=%d out-of-stock=%d faults: relay=%d before-commit=%d before-ack=%d duplicates=%d",
		confirmed, declined, outOfStock, relayCrashes.Load(), commitCrashes.Load(), ackCrashes.Load(), s.duplicates())
	require.NotZero(t, confirmed)
	require.NotZero(t, declined, "compensation was never exercised")
	require.NotZero(t, outOfStock)
	require.NotZero(t, relayCrashes.Load()+commitCrashes.Load()+ackCrashes.Load())
	require.NotZero(t, s.duplicates())

	// One effect per event, no matter how often it was delivered.
	require.Equal(t, reserved, s.count(t, s.inv.DB, `SELECT COUNT(*) FROM reservations`))
	require.Equal(t, reserved, s.count(t, s.pay.DB, `SELECT COUNT(*) FROM payments`))
	require.Zero(t, s.count(t, s.pay.DB, `SELECT COUNT(*) FROM (SELECT 1 FROM payments GROUP BY order_id HAVING COUNT(*) > 1)`))
	require.Equal(t, initial, s.count(t, s.inv.DB, `SELECT SUM(available + reserved + sold) FROM stock`))
}

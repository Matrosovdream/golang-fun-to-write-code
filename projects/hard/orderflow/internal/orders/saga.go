// Package orders owns the order lifecycle and orchestrates the saga.
//
// There is no distributed transaction across orders, inventory and payments,
// so an order is a saga: a sequence of local transactions, each followed by a
// message that triggers the next one. When a step fails, the steps already
// taken are undone by compensating actions instead of a rollback:
//
//	PENDING ──stock reserved──▶ STOCK_RESERVED ──payment ok──▶ PAID ──stock committed──▶ CONFIRMED
//	   │                              │
//	   │ stock rejected               │ payment declined: release the stock
//	   ▼                              ▼
//	CANCELLED ◀──stock released── CANCELLING
//
// This service is the orchestrator: it alone knows the whole flow, and
// inventory and payments just answer commands. (In a choreographed saga
// each service would react to the others' events and nobody would hold the
// flow; the README contrasts the two.) Because the orchestrator sends one
// command per order and waits for its reply before sending the next, the
// messages of one order are ordered by construction. No subject keying or
// sequence numbers are needed; only different orders interleave.
package orders

import (
	"errors"
	"fmt"
	"slices"

	"orderflow/internal/event"
)

type State string

const (
	Pending       State = "PENDING"
	StockReserved State = "STOCK_RESERVED"
	Paid          State = "PAID"
	Confirmed     State = "CONFIRMED"
	Cancelling    State = "CANCELLING"
	Cancelled     State = "CANCELLED"
)

var ErrIllegalTransition = errors.New("illegal state transition")

// transitions is the only place that says which moves are legal.
var transitions = map[State][]State{
	Pending:       {StockReserved, Cancelled},
	StockReserved: {Paid, Cancelling},
	Paid:          {Confirmed},
	Cancelling:    {Cancelled},
}

func (s State) CanGo(to State) bool { return slices.Contains(transitions[s], to) }

func (s State) Terminal() bool { return len(transitions[s]) == 0 }

func Transition(from, to State) error {
	if !from.CanGo(to) {
		return fmt.Errorf("%w: %s -> %s", ErrIllegalTransition, from, to)
	}
	return nil
}

// reaction is what the orchestrator does when a reply arrives: move to a
// state and, unless the saga is over, send the next command in the same
// transaction.
type reaction struct {
	to   State
	next string
}

var reactions = map[string]reaction{
	event.StockReserved:    {StockReserved, event.Charge},
	event.StockRejected:    {Cancelled, ""},
	event.PaymentSucceeded: {Paid, event.Commit},
	event.PaymentDeclined:  {Cancelling, event.Release},
	event.StockCommitted:   {Confirmed, ""},
	event.StockReleased:    {Cancelled, ""},
}

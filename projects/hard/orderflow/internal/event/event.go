// Package event defines the envelope every message on the bus travels in,
// plus the payloads of the saga's commands and replies.
package event

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Type doubles as the NATS subject, so its first token picks the stream:
// inventory.* lands in the inventory stream, and so on.
type Event struct {
	ID            string          `json:"id"`
	Type          string          `json:"type"`
	AggregateID   string          `json:"aggregate_id"`   // the order this is about
	CorrelationID string          `json:"correlation_id"` // shared by every event of one saga
	Time          time.Time       `json:"time"`
	Data          json.RawMessage `json:"data"`
}

// Commands flow from the orders service to inventory and payments; replies
// flow back on orders.*.
const (
	Reserve = "inventory.reserve"
	Commit  = "inventory.commit"
	Release = "inventory.release"
	Charge  = "payments.charge"

	StockReserved    = "orders.stock_reserved"
	StockRejected    = "orders.stock_rejected"
	StockCommitted   = "orders.stock_committed"
	StockReleased    = "orders.stock_released"
	PaymentSucceeded = "orders.payment_succeeded"
	PaymentDeclined  = "orders.payment_declined"
)

type Cmd struct {
	OrderID string `json:"order_id"`
	SKU     string `json:"sku"`
	Qty     int    `json:"qty"`
	Amount  int    `json:"amount"`
}

type Reply struct {
	OrderID string `json:"order_id"`
	Reason  string `json:"reason,omitempty"`
}

func NewID(prefix string) string {
	b := make([]byte, 8)
	rand.Read(b)
	return prefix + "_" + hex.EncodeToString(b)
}

func New(typ, aggregateID, correlationID string, data any) (Event, error) {
	b, err := json.Marshal(data)
	if err != nil {
		return Event{}, fmt.Errorf("marshal %s data: %w", typ, err)
	}
	return Event{
		ID: NewID("evt"), Type: typ, AggregateID: aggregateID,
		CorrelationID: correlationID, Time: time.Now().UTC(), Data: b,
	}, nil
}

// Decode rejects envelopes without an ID or type: the ID is the inbox key,
// so a message without one can never be processed safely.
func Decode(b []byte) (Event, error) {
	var e Event
	if err := json.Unmarshal(b, &e); err != nil {
		return e, fmt.Errorf("decode envelope: %w", err)
	}
	if e.ID == "" || e.Type == "" {
		return e, errors.New("decode envelope: missing id or type")
	}
	return e, nil
}

func Data[T any](e Event) (T, error) {
	var v T
	if err := json.Unmarshal(e.Data, &v); err != nil {
		return v, fmt.Errorf("decode %s data: %w", e.Type, err)
	}
	return v, nil
}

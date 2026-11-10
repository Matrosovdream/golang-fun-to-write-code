package event

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewAndDecodeRoundTrip(t *testing.T) {
	ev, err := New(Reserve, "ord_1", "cor_1", Cmd{OrderID: "ord_1", SKU: "widget", Qty: 2, Amount: 200})
	require.NoError(t, err)
	require.NotEmpty(t, ev.ID)
	require.False(t, ev.Time.IsZero())

	b, err := json.Marshal(ev)
	require.NoError(t, err)
	got, err := Decode(b)
	require.NoError(t, err)
	require.Equal(t, ev.ID, got.ID)
	require.Equal(t, "cor_1", got.CorrelationID)

	cmd, err := Data[Cmd](got)
	require.NoError(t, err)
	require.Equal(t, Cmd{OrderID: "ord_1", SKU: "widget", Qty: 2, Amount: 200}, cmd)
}

func TestDecodeRejectsMalformedEnvelopes(t *testing.T) {
	for name, in := range map[string]string{
		"not json":   `{nope`,
		"empty":      ``,
		"no id":      `{"type":"orders.x"}`,
		"no type":    `{"id":"evt_1"}`,
		"wrong type": `{"id":42,"type":"orders.x"}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Decode([]byte(in))
			require.Error(t, err)
		})
	}
}

func TestIDsAreUnique(t *testing.T) {
	seen := map[string]bool{}
	for range 1000 {
		id := NewID("evt")
		require.False(t, seen[id])
		seen[id] = true
	}
}

package check

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"orderflow/internal/inventory"
	"orderflow/internal/orders"
)

func TestInvariants(t *testing.T) {
	order := func(id string, st orders.State, qty int) orders.Order {
		return orders.Order{ID: id, SKU: "w", Qty: qty, State: st}
	}
	tests := []struct {
		name  string
		all   []orders.Order
		stock inventory.Stock
		want  string // substring expected among the violations; "" means none
	}{
		{"settled and consistent",
			[]orders.Order{order("1", orders.Confirmed, 3), order("2", orders.Cancelled, 5)},
			inventory.Stock{SKU: "w", Initial: 10, Available: 7, Sold: 3}, ""},
		{"order stuck", []orders.Order{order("1", orders.Paid, 3)},
			inventory.Stock{SKU: "w", Initial: 10, Available: 7, Reserved: 3}, "stuck in PAID"},
		{"negative bucket", nil,
			inventory.Stock{SKU: "w", Initial: 10, Available: -1, Sold: 11}, "negative"},
		{"units vanished", nil,
			inventory.Stock{SKU: "w", Initial: 10, Available: 9}, "!= initial"},
		{"reserved leak", []orders.Order{order("1", orders.Cancelled, 2)},
			inventory.Stock{SKU: "w", Initial: 10, Available: 8, Reserved: 2}, "still reserved"},
		{"reserved leak with a confirmed order", []orders.Order{order("1", orders.Confirmed, 2)},
			inventory.Stock{SKU: "w", Initial: 10, Available: 8, Sold: 0, Reserved: 2}, "still reserved"},
		{"sold without a confirmed order", []orders.Order{order("1", orders.Cancelled, 2)},
			inventory.Stock{SKU: "w", Initial: 10, Available: 8, Sold: 2}, "confirmed 0 + available 8"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Invariants(tc.all, []inventory.Stock{tc.stock})
			if tc.want == "" {
				require.Empty(t, got)
				return
			}
			require.Contains(t, strings.Join(got, "\n"), tc.want)
		})
	}
}

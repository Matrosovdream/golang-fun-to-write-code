// Package check states the pipeline's invariants in one place, so the chaos
// tests and the placeorders demo assert exactly the same things.
package check

import (
	"fmt"

	"orderflow/internal/inventory"
	"orderflow/internal/orders"
)

// Invariants returns one message per violation; empty means every invariant
// holds. The last two only make sense once every order is terminal, because
// until then units are legitimately in flight.
//
//   - no stock bucket is negative and available+reserved+sold == initial
//   - every order is terminal (CONFIRMED or CANCELLED)
//   - nothing is left reserved
//   - sum of confirmed quantities + remaining stock == initial stock
func Invariants(all []orders.Order, stock []inventory.Stock) []string {
	var bad []string
	add := func(format string, args ...any) { bad = append(bad, fmt.Sprintf(format, args...)) }

	confirmed := map[string]int{}
	settled := true
	for _, o := range all {
		if !o.State.Terminal() {
			settled = false
			add("order %s stuck in %s", o.ID, o.State)
		}
		if o.State == orders.Confirmed {
			confirmed[o.SKU] += o.Qty
		}
	}
	for _, s := range stock {
		if s.Available < 0 || s.Reserved < 0 || s.Sold < 0 {
			add("%s: negative stock bucket %+v", s.SKU, s)
		}
		if s.Available+s.Reserved+s.Sold != s.Initial {
			add("%s: available %d + reserved %d + sold %d != initial %d", s.SKU, s.Available, s.Reserved, s.Sold, s.Initial)
		}
		if !settled {
			continue
		}
		if s.Reserved != 0 {
			add("%s: %d units still reserved although every order is terminal", s.SKU, s.Reserved)
		}
		if confirmed[s.SKU]+s.Available != s.Initial {
			add("%s: confirmed %d + available %d != initial %d", s.SKU, confirmed[s.SKU], s.Available, s.Initial)
		}
	}
	return bad
}

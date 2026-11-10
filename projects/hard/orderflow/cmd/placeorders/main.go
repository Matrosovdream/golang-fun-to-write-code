// Command placeorders POSTs N random orders to a running orderflow, waits
// until every order is terminal, prints the final state table and checks
// the pipeline's invariants. Exit status 1 means one was violated.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math/rand/v2"
	"net/http"
	"os"
	"os/signal"
	"text/tabwriter"
	"time"

	"orderflow/internal/check"
	"orderflow/internal/inventory"
	"orderflow/internal/orders"
)

func main() {
	base := flag.String("url", "http://127.0.0.1:8107", "orderflow base URL")
	n := flag.Int("n", 20, "number of orders to place")
	seed := flag.Uint64("seed", 1, "seed for the random SKU and quantity choices")
	wait := flag.Duration("wait", 30*time.Second, "how long to wait for every order to finish")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx, *base, *n, *seed, *wait); err != nil {
		fmt.Fprintln(os.Stderr, "placeorders:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, base string, n int, seed uint64, wait time.Duration) error {
	stock, err := call[[]inventory.Stock](ctx, "GET", base+"/stock", nil)
	if err != nil || len(stock) == 0 {
		return fmt.Errorf("reading stock: %v (%d SKUs)", err, len(stock))
	}
	rng := rand.New(rand.NewPCG(seed, seed))
	for range n {
		req := orders.CreateRequest{SKU: stock[rng.IntN(len(stock))].SKU, Qty: 1 + rng.IntN(3)}
		req.Amount = 100 * req.Qty
		if _, err := call[orders.Order](ctx, "POST", base+"/orders", req); err != nil {
			return err
		}
	}

	wctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	var all []orders.Order
	for settled := false; !settled && wctx.Err() == nil; {
		if all, err = call[[]orders.Order](wctx, "GET", base+"/orders", nil); err != nil {
			return err
		}
		settled = true
		for _, o := range all {
			settled = settled && o.State.Terminal()
		}
		if !settled {
			time.Sleep(100 * time.Millisecond)
		}
	}
	if stock, err = call[[]inventory.Stock](ctx, "GET", base+"/stock", nil); err != nil {
		return err
	}
	return report(all, stock)
}

func report(all []orders.Order, stock []inventory.Stock) error {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ORDER\tSKU\tQTY\tSTATE\tREASON")
	confirmed := 0
	for _, o := range all {
		fmt.Fprintf(w, "%s\t%s\t%d\t%s\t%s\n", o.ID, o.SKU, o.Qty, o.State, o.Reason)
		if o.State == orders.Confirmed {
			confirmed++
		}
	}
	fmt.Fprintln(w, "\nSKU\tINITIAL\tAVAILABLE\tRESERVED\tSOLD")
	for _, s := range stock {
		fmt.Fprintf(w, "%s\t%d\t%d\t%d\t%d\n", s.SKU, s.Initial, s.Available, s.Reserved, s.Sold)
	}
	w.Flush()

	fmt.Printf("\n%d orders: %d confirmed, %d cancelled\n", len(all), confirmed, len(all)-confirmed)
	bad := check.Invariants(all, stock)
	for _, msg := range bad {
		fmt.Println("invariant violated:", msg)
	}
	if len(bad) > 0 {
		return fmt.Errorf("%d invariants violated", len(bad))
	}
	fmt.Println("invariants: OK (stock never negative, every order terminal, nothing reserved, confirmed + available == initial)")
	return nil
}

// call sends one request (JSON body when non-nil) and decodes the JSON reply.
func call[T any](ctx context.Context, method, url string, body any) (v T, err error) {
	var in bytes.Buffer
	if body != nil {
		if err = json.NewEncoder(&in).Encode(body); err != nil {
			return v, err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, url, &in)
	if err != nil {
		return v, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return v, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return v, fmt.Errorf("%s %s: %s", method, url, resp.Status)
	}
	return v, json.NewDecoder(resp.Body).Decode(&v)
}

// A demo client that exercises every RPC shape against running servers.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	inventoryv1 "grpcinv/gen/inventory/v1"
)

func main() {
	invAddr := flag.String("inventory", "localhost:9091", "inventory address")
	ordAddr := flag.String("orders", "localhost:9092", "orders address")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	invConn := dial(*invAddr)
	defer invConn.Close()
	ordConn := dial(*ordAddr)
	defer ordConn.Close()
	inv := inventoryv1.NewInventoryClient(invConn)
	ord := inventoryv1.NewOrdersClient(ordConn)

	// 1. Client streaming: upload the catalog.
	up, err := inv.BulkAddProducts(ctx)
	check(err)
	for _, p := range []*inventoryv1.Product{
		{Sku: "gopher-tee", Name: "Gopher T-Shirt", PriceCents: 2500, Stock: 20},
		{Sku: "gopher-mug", Name: "Gopher Mug", PriceCents: 1200, Stock: 5},
		{Sku: "", Name: "broken row", PriceCents: 1, Stock: 1}, // rejected
	} {
		check(up.Send(&inventoryv1.AddProductRequest{Product: p}))
	}
	summary, err := up.CloseAndRecv()
	check(err)
	fmt.Printf("bulk upload: added=%d rejected=%d\n", summary.GetAdded(), summary.GetRejected())

	// 2. Server streaming: watch stock changes in the background.
	watch, err := inv.WatchStock(ctx, &inventoryv1.WatchStockRequest{Sku: "gopher-mug"})
	check(err)
	go func() {
		for {
			u, err := watch.Recv()
			if err != nil {
				return
			}
			fmt.Printf("  [watch] gopher-mug stock -> %d\n", u.GetStock())
		}
	}()

	// 3. Unary through the Orders service (which calls Inventory itself).
	order, err := ord.PlaceOrder(ctx, &inventoryv1.PlaceOrderRequest{Sku: "gopher-mug", Quantity: 2})
	check(err)
	fmt.Printf("placed %s: %d × gopher-mug = $%.2f\n",
		order.GetId(), order.GetQuantity(), float64(order.GetTotalCents())/100)

	// 4. Bidi streaming: interactive reservations on one stream.
	res, err := inv.Reserve(ctx)
	check(err)
	for _, q := range []int64{1, 100} {
		check(res.Send(&inventoryv1.ReserveRequest{Sku: "gopher-mug", Quantity: q}))
		r, err := res.Recv()
		check(err)
		fmt.Printf("reserve %d: ok=%v remaining=%d %s\n", q, r.GetOk(), r.GetRemainingStock(), r.GetReason())
	}
	check(res.CloseSend())

	time.Sleep(300 * time.Millisecond) // let the watcher print its last update
}

func dial(addr string) *grpc.ClientConn {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	check(err)
	return conn
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

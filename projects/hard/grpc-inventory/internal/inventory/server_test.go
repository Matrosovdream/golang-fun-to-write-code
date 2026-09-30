package inventory_test

import (
	"context"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	inventoryv1 "grpcinv/gen/inventory/v1"
	"grpcinv/internal/inventory"
)

// startServer runs the real gRPC stack over bufconn: an in-memory listener,
// no TCP ports, no flakiness — the standard way to test gRPC services.
func startServer(t *testing.T) inventoryv1.InventoryClient {
	t.Helper()

	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer()
	inventoryv1.RegisterInventoryServer(srv,
		inventory.New(slog.New(slog.NewTextHandler(io.Discard, nil))))
	go srv.Serve(lis) //nolint:errcheck
	t.Cleanup(srv.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { conn.Close() })

	return inventoryv1.NewInventoryClient(conn)
}

func seed(t *testing.T, client inventoryv1.InventoryClient, products ...*inventoryv1.Product) {
	t.Helper()
	up, err := client.BulkAddProducts(context.Background())
	require.NoError(t, err)
	for _, p := range products {
		require.NoError(t, up.Send(&inventoryv1.AddProductRequest{Product: p}))
	}
	_, err = up.CloseAndRecv()
	require.NoError(t, err)
}

func TestGetProduct(t *testing.T) {
	client := startServer(t)
	seed(t, client, &inventoryv1.Product{Sku: "tee", Name: "T-Shirt", PriceCents: 2500, Stock: 3})

	p, err := client.GetProduct(context.Background(), &inventoryv1.GetProductRequest{Sku: "tee"})
	require.NoError(t, err)
	require.Equal(t, "T-Shirt", p.GetName())

	// gRPC error contract: the code matters, not the message text.
	_, err = client.GetProduct(context.Background(), &inventoryv1.GetProductRequest{Sku: "nope"})
	require.Equal(t, codes.NotFound, status.Code(err))
}

func TestBulkAddRejectsInvalid(t *testing.T) {
	client := startServer(t)

	up, err := client.BulkAddProducts(context.Background())
	require.NoError(t, err)
	require.NoError(t, up.Send(&inventoryv1.AddProductRequest{
		Product: &inventoryv1.Product{Sku: "ok", Stock: 1}}))
	require.NoError(t, up.Send(&inventoryv1.AddProductRequest{
		Product: &inventoryv1.Product{Sku: "", Stock: 1}})) // no sku → rejected

	summary, err := up.CloseAndRecv()
	require.NoError(t, err)
	require.EqualValues(t, 1, summary.GetAdded())
	require.EqualValues(t, 1, summary.GetRejected())
}

func TestBidiReserveDrainsStock(t *testing.T) {
	client := startServer(t)
	seed(t, client, &inventoryv1.Product{Sku: "mug", Stock: 3})

	stream, err := client.Reserve(context.Background())
	require.NoError(t, err)

	tests := []struct {
		quantity  int64
		wantOK    bool
		remaining int64
	}{
		{2, true, 1},
		{2, false, 1}, // only 1 left
		{1, true, 0},
	}
	for _, tc := range tests {
		require.NoError(t, stream.Send(&inventoryv1.ReserveRequest{Sku: "mug", Quantity: tc.quantity}))
		res, err := stream.Recv()
		require.NoError(t, err)
		require.Equal(t, tc.wantOK, res.GetOk())
		require.EqualValues(t, tc.remaining, res.GetRemainingStock())
	}
	require.NoError(t, stream.CloseSend())
}

func TestWatchStockStreamsUpdates(t *testing.T) {
	client := startServer(t)
	seed(t, client, &inventoryv1.Product{Sku: "tee", Stock: 10})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	watch, err := client.WatchStock(ctx, &inventoryv1.WatchStockRequest{Sku: "tee"})
	require.NoError(t, err)
	time.Sleep(100 * time.Millisecond) // let the subscription register

	_, err = client.ReserveStock(ctx, &inventoryv1.ReserveRequest{Sku: "tee", Quantity: 4})
	require.NoError(t, err)

	update, err := watch.Recv()
	require.NoError(t, err)
	require.Equal(t, "tee", update.GetSku())
	require.EqualValues(t, 6, update.GetStock())
}

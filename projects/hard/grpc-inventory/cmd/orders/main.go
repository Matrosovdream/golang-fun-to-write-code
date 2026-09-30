package main

import (
	"flag"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	inventoryv1 "grpcinv/gen/inventory/v1"
	"grpcinv/internal/interceptor"
	"grpcinv/internal/orders"
)

func main() {
	addr := flag.String("addr", ":9092", "listen address")
	invAddr := flag.String("inventory", "localhost:9091", "inventory service address")
	flag.Parse()
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))

	// NewClient connects lazily; failures surface per-RPC, which is what the
	// circuit breaker is for. TLS creds would replace insecure here.
	conn, err := grpc.NewClient(*invAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Error("dial inventory", "err", err)
		os.Exit(1)
	}
	defer conn.Close()

	lis, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Error("listen", "err", err)
		os.Exit(1)
	}

	srv := grpc.NewServer(
		grpc.ChainUnaryInterceptor(interceptor.Recovery(log), interceptor.Logging(log)),
	)
	inventoryv1.RegisterOrdersServer(srv, orders.New(inventoryv1.NewInventoryClient(conn), log))

	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
		<-sig
		srv.GracefulStop()
	}()

	log.Info("orders listening", "addr", *addr, "inventory", *invAddr)
	if err := srv.Serve(lis); err != nil {
		log.Error("serve", "err", err)
		os.Exit(1)
	}
}

package main

import (
	"flag"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthv1 "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"

	inventoryv1 "grpcinv/gen/inventory/v1"
	"grpcinv/internal/interceptor"
	"grpcinv/internal/inventory"
)

func main() {
	addr := flag.String("addr", ":9091", "listen address")
	flag.Parse()
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))

	lis, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Error("listen", "err", err)
		os.Exit(1)
	}

	srv := grpc.NewServer(
		// ChainUnaryInterceptor runs them in order: recovery outermost so a
		// panic in the logger itself is still caught.
		grpc.ChainUnaryInterceptor(interceptor.Recovery(log), interceptor.Logging(log)),
		grpc.ChainStreamInterceptor(interceptor.StreamLogging(log)),
	)
	inventoryv1.RegisterInventoryServer(srv, inventory.New(log))
	// Standard extras every real gRPC service ships:
	healthv1.RegisterHealthServer(srv, health.NewServer()) // for load balancers / k8s probes
	reflection.Register(srv)                               // lets grpcurl explore the API

	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
		<-sig
		log.Info("draining")
		srv.GracefulStop() // finish in-flight RPCs, refuse new ones
	}()

	log.Info("inventory listening", "addr", *addr)
	if err := srv.Serve(lis); err != nil {
		log.Error("serve", "err", err)
		os.Exit(1)
	}
}

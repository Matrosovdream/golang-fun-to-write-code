// Package orders implements the Orders service: it depends on Inventory
// over gRPC — a service calling a service, with deadlines and a circuit
// breaker on the client side.
package orders

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	inventoryv1 "grpcinv/gen/inventory/v1"
)

type Server struct {
	inventoryv1.UnimplementedOrdersServer

	inventory inventoryv1.InventoryClient
	breaker   *Breaker
	log       *slog.Logger
	nextID    atomic.Int64
}

func New(inventory inventoryv1.InventoryClient, log *slog.Logger) *Server {
	return &Server{
		inventory: inventory,
		breaker:   NewBreaker(3, 5*time.Second),
		log:       log,
	}
}

func (s *Server) PlaceOrder(ctx context.Context, req *inventoryv1.PlaceOrderRequest) (*inventoryv1.Order, error) {
	if req.GetQuantity() <= 0 {
		return nil, status.Error(codes.InvalidArgument, "quantity must be positive")
	}

	// Deadline discipline: our caller's deadline propagates through ctx, and
	// we cap our downstream calls even if the caller set none.
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	ctx = metadata.AppendToOutgoingContext(ctx, "x-request-id",
		fmt.Sprintf("order-%d", time.Now().UnixNano()))

	var product *inventoryv1.Product
	err := s.breaker.Do(func() error {
		var err error
		product, err = s.inventory.GetProduct(ctx, &inventoryv1.GetProductRequest{Sku: req.GetSku()})
		return err
	})
	if err != nil {
		return nil, translate(err)
	}

	var result *inventoryv1.ReserveResult
	err = s.breaker.Do(func() error {
		var err error
		result, err = s.inventory.ReserveStock(ctx, &inventoryv1.ReserveRequest{
			Sku: req.GetSku(), Quantity: req.GetQuantity(),
		})
		return err
	})
	if err != nil {
		return nil, translate(err)
	}
	if !result.GetOk() {
		return nil, status.Errorf(codes.FailedPrecondition, "cannot reserve: %s", result.GetReason())
	}

	return &inventoryv1.Order{
		Id:         fmt.Sprintf("ord-%06d", s.nextID.Add(1)),
		Sku:        req.GetSku(),
		Quantity:   req.GetQuantity(),
		TotalCents: product.GetPriceCents() * req.GetQuantity(),
		PlacedAt:   timestamppb.Now(),
	}, nil
}

// translate keeps downstream status codes meaningful to our caller instead
// of wrapping everything in codes.Internal.
func translate(err error) error {
	switch status.Code(err) {
	case codes.NotFound, codes.InvalidArgument, codes.DeadlineExceeded, codes.Unavailable:
		return err
	default:
		if _, open := err.(*BreakerOpenError); open {
			return status.Error(codes.Unavailable, err.Error())
		}
		return status.Errorf(codes.Internal, "inventory call failed: %v", err)
	}
}

// Package inventory implements the Inventory gRPC service: an in-memory
// product store with live stock subscriptions.
package inventory

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	inventoryv1 "grpcinv/gen/inventory/v1"
)

type Server struct {
	// Forward compatibility: new RPCs added to the proto later return
	// Unimplemented instead of breaking the build.
	inventoryv1.UnimplementedInventoryServer

	log *slog.Logger

	mu       sync.RWMutex
	products map[string]*inventoryv1.Product
	watchers map[string][]chan *inventoryv1.StockUpdate
}

func New(log *slog.Logger) *Server {
	return &Server{
		log:      log,
		products: make(map[string]*inventoryv1.Product),
		watchers: make(map[string][]chan *inventoryv1.StockUpdate),
	}
}

// GetProduct — unary. Errors travel as status codes: NotFound here becomes
// codes.NotFound on the client, not a string to parse.
func (s *Server) GetProduct(_ context.Context, req *inventoryv1.GetProductRequest) (*inventoryv1.Product, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	p, ok := s.products[req.GetSku()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "sku %q not found", req.GetSku())
	}
	// Return a copy: the map's value keeps changing under its own lock.
	return clone(p), nil
}

func (s *Server) ReserveStock(_ context.Context, req *inventoryv1.ReserveRequest) (*inventoryv1.ReserveResult, error) {
	if req.GetQuantity() <= 0 {
		return nil, status.Error(codes.InvalidArgument, "quantity must be positive")
	}
	return s.reserve(req), nil
}

// BulkAddProducts — client streaming: consume until io.EOF, then reply once.
func (s *Server) BulkAddProducts(stream grpc.ClientStreamingServer[inventoryv1.AddProductRequest, inventoryv1.BulkAddSummary]) error {
	var summary inventoryv1.BulkAddSummary
	for {
		req, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return stream.SendAndClose(&summary)
		}
		if err != nil {
			return err
		}

		p := req.GetProduct()
		if p.GetSku() == "" || p.GetStock() < 0 {
			summary.Rejected++
			continue
		}
		s.mu.Lock()
		s.products[p.GetSku()] = clone(p)
		s.mu.Unlock()
		summary.Added++
		s.notify(p.GetSku(), p.GetStock())
	}
}

// WatchStock — server streaming: subscribe and forward until the client goes
// away. Stream lifetime == subscription lifetime, ended by ctx.
func (s *Server) WatchStock(req *inventoryv1.WatchStockRequest, stream grpc.ServerStreamingServer[inventoryv1.StockUpdate]) error {
	sku := req.GetSku()

	updates := make(chan *inventoryv1.StockUpdate, 16)
	s.mu.Lock()
	s.watchers[sku] = append(s.watchers[sku], updates)
	s.mu.Unlock()
	defer s.unsubscribe(sku, updates)

	for {
		select {
		case u := <-updates:
			if err := stream.Send(u); err != nil {
				return err
			}
		case <-stream.Context().Done():
			// The client cancelled or its deadline passed — normal end.
			return nil
		}
	}
}

// Reserve — bidirectional streaming: each request gets exactly one result,
// in order, over the same stream.
func (s *Server) Reserve(stream grpc.BidiStreamingServer[inventoryv1.ReserveRequest, inventoryv1.ReserveResult]) error {
	for {
		req, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := stream.Send(s.reserve(req)); err != nil {
			return err
		}
	}
}

func (s *Server) reserve(req *inventoryv1.ReserveRequest) *inventoryv1.ReserveResult {
	s.mu.Lock()
	defer s.mu.Unlock()

	p, ok := s.products[req.GetSku()]
	switch {
	case !ok:
		return &inventoryv1.ReserveResult{Sku: req.GetSku(), Ok: false, Reason: "unknown sku"}
	case p.Stock < req.GetQuantity():
		return &inventoryv1.ReserveResult{Sku: req.GetSku(), Ok: false, RemainingStock: p.Stock, Reason: "insufficient stock"}
	}
	p.Stock -= req.GetQuantity()
	go s.notify(p.GetSku(), p.GetStock())
	return &inventoryv1.ReserveResult{Sku: req.GetSku(), Ok: true, RemainingStock: p.Stock}
}

func (s *Server) notify(sku string, stock int64) {
	update := &inventoryv1.StockUpdate{Sku: sku, Stock: stock, At: timestamppb.Now()}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, ch := range s.watchers[sku] {
		select {
		case ch <- update:
		default: // a stalled watcher loses updates, never blocks the store
		}
	}
}

func (s *Server) unsubscribe(sku string, ch chan *inventoryv1.StockUpdate) {
	s.mu.Lock()
	defer s.mu.Unlock()
	watchers := s.watchers[sku]
	for i, c := range watchers {
		if c == ch {
			s.watchers[sku] = append(watchers[:i], watchers[i+1:]...)
			return
		}
	}
}

func clone(p *inventoryv1.Product) *inventoryv1.Product {
	return &inventoryv1.Product{Sku: p.GetSku(), Name: p.GetName(), PriceCents: p.GetPriceCents(), Stock: p.GetStock()}
}

package catalog

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

type Service struct {
	repo   Repository
	tracer trace.Tracer
	log    *slog.Logger
}

func (s *Service) Get(ctx context.Context, id string) (Product, error) {
	ctx, span := s.tracer.Start(ctx, "catalog.Get")
	defer span.End()
	p, err := s.repo.Get(ctx, id)
	if err != nil {
		recordErr(span, err)
		s.log.WarnContext(ctx, "get product failed", "id", id, "err", err)
		return Product{}, fmt.Errorf("get product: %w", err)
	}
	return p, nil
}

func (s *Service) List(ctx context.Context) ([]Product, error) {
	ctx, span := s.tracer.Start(ctx, "catalog.List")
	defer span.End()
	ps, err := s.repo.List(ctx)
	if err != nil {
		recordErr(span, err)
		s.log.WarnContext(ctx, "list products failed", "err", err)
		return nil, fmt.Errorf("list products: %w", err)
	}
	return ps, nil
}

// tracedRepo is the decorator: it wraps a Repository and is one, so the real
// repo carries no tracing code and the service cannot tell the difference.
type tracedRepo struct {
	next   Repository
	tracer trace.Tracer
}

func (t tracedRepo) Get(ctx context.Context, id string) (Product, error) {
	ctx, span := t.tracer.Start(ctx, "repo.Get", trace.WithAttributes(attribute.String("product.id", id)))
	defer span.End()
	p, err := t.next.Get(ctx, id)
	recordErr(span, err)
	return p, err
}

func (t tracedRepo) List(ctx context.Context) ([]Product, error) {
	ctx, span := t.tracer.Start(ctx, "repo.List")
	defer span.End()
	ps, err := t.next.List(ctx)
	recordErr(span, err)
	return ps, err
}

// recordErr separates the caller's mistake from our failure: an unknown id is an
// event and the span stays OK; anything else is an "exception" event plus status Error.
func recordErr(span trace.Span, err error) {
	switch {
	case err == nil:
	case errors.Is(err, ErrNotFound):
		span.AddEvent("rejected", trace.WithAttributes(attribute.String("reason", err.Error())))
	default:
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
}

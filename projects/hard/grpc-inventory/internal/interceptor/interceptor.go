// Package interceptor holds gRPC middleware — the same idea as HTTP
// middleware, but unary and stream calls need separate wrappers.
package interceptor

import (
	"context"
	"log/slog"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func Logging(log *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		start := time.Now()
		resp, err := handler(ctx, req)

		// Metadata is gRPC's headers; requestID shows the client→server flow.
		requestID := ""
		if md, ok := metadata.FromIncomingContext(ctx); ok {
			if v := md.Get("x-request-id"); len(v) > 0 {
				requestID = v[0]
			}
		}
		log.Info("rpc",
			"method", info.FullMethod,
			"code", status.Code(err).String(),
			"duration", time.Since(start).Round(time.Microsecond),
			"request_id", requestID,
		)
		return resp, err
	}
}

func StreamLogging(log *slog.Logger) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		start := time.Now()
		err := handler(srv, ss)
		log.Info("stream rpc",
			"method", info.FullMethod,
			"code", status.Code(err).String(),
			"duration", time.Since(start).Round(time.Millisecond),
		)
		return err
	}
}

// Recovery converts panics into codes.Internal instead of killing the whole
// process — one broken handler must not take down every open stream.
func Recovery(log *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
		defer func() {
			if r := recover(); r != nil {
				log.Error("panic in handler", "method", info.FullMethod, "panic", r)
				err = status.Errorf(codes.Internal, "internal error")
			}
		}()
		return handler(ctx, req)
	}
}

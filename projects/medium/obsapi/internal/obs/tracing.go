package obs

import (
	"context"
	"os"

	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
)

// Propagator is passed explicitly, not installed globally, so two services can
// share a process (as the tests do) without sharing hidden state.
var Propagator propagation.TextMapPropagator = propagation.TraceContext{}

// NewExporter is the only place that knows where spans go.
func NewExporter(ctx context.Context) (sdktrace.SpanExporter, error) {
	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != "" {
		return otlptracehttp.New(ctx) // endpoint, headers, timeouts: standard OTEL_* env vars
	}
	return stdouttrace.New()
}

// NewTracerProvider: ParentBased means a service follows the sampling decision
// made upstream, so a trace is never stored half-way. Only a request that
// arrives without a parent is sampled at ratio.
func NewTracerProvider(service string, ratio float64, proc sdktrace.SpanProcessor) *sdktrace.TracerProvider {
	return sdktrace.NewTracerProvider(
		sdktrace.WithResource(resource.NewSchemaless(semconv.ServiceName(service))),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(ratio))),
		sdktrace.WithSpanProcessor(proc),
	)
}

// -------------------------------------------------------------------------------
// Tracing - OpenTelemetry Initialization and Span Helpers
//
// Author: Alex Freidah
//
// Initializes the OpenTelemetry tracer provider with OTLP gRPC export and
// provides span creation helpers for server, client, and internal operations.
// Includes custom attribute keys prefixed with g3. for domain-specific trace
// metadata.
// -------------------------------------------------------------------------------

package telemetry

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	"go.opentelemetry.io/otel/trace"
)

// -------------------------------------------------------------------------
// CONSTANTS
// -------------------------------------------------------------------------

// TracerName identifies the tracer in OpenTelemetry output.
const TracerName = "g3"

// Version is set at build time via ldflags.
var Version = "dev"

// -------------------------------------------------------------------------
// CUSTOM ATTRIBUTES
// -------------------------------------------------------------------------

var (
	AttrRequestID  = attribute.Key("g3.request_id")
	AttrBucket     = attribute.Key("g3.bucket")
	AttrObjectKey  = attribute.Key("g3.key")
	AttrOperation  = attribute.Key("g3.operation")
	AttrObjectSize = attribute.Key("g3.object.size")
	AttrContentType = attribute.Key("g3.object.content_type")
	AttrGmailMsgID = attribute.Key("g3.gmail.message_id")
	AttrChunkCount = attribute.Key("g3.chunk.count")
	AttrChunkIndex = attribute.Key("g3.chunk.index")
)

// -------------------------------------------------------------------------
// INITIALIZATION
// -------------------------------------------------------------------------

// InitTracer initializes the OpenTelemetry tracer provider and returns a
// shutdown function. If tracing is disabled, the shutdown function is a no-op.
func InitTracer(ctx context.Context, cfg TracingConfig) (func(context.Context) error, error) {
	if !cfg.Enabled {
		return func(context.Context) error { return nil }, nil
	}

	opts := []otlptracegrpc.Option{
		otlptracegrpc.WithEndpoint(cfg.Endpoint),
	}
	if cfg.Insecure {
		opts = append(opts, otlptracegrpc.WithInsecure())
	}

	exporter, err := otlptracegrpc.New(ctx, opts...)
	if err != nil {
		return nil, err
	}

	res, err := resource.Merge(
		resource.Default(),
		resource.NewSchemaless(
			semconv.ServiceName(TracerName),
			semconv.ServiceVersion(Version),
		),
	)
	if err != nil {
		return nil, err
	}

	var sampler sdktrace.Sampler
	switch {
	case cfg.SampleRate >= 1.0:
		sampler = sdktrace.AlwaysSample()
	case cfg.SampleRate <= 0:
		sampler = sdktrace.NeverSample()
	default:
		sampler = sdktrace.TraceIDRatioBased(cfg.SampleRate)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sampler),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return tp.Shutdown, nil
}

// -------------------------------------------------------------------------
// SPAN HELPERS
// -------------------------------------------------------------------------

// Tracer returns the global tracer for g3.
func Tracer() trace.Tracer {
	return otel.Tracer(TracerName)
}

// StartSpan creates a new span with the given name and attributes.
func StartSpan(ctx context.Context, name string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	return Tracer().Start(ctx, name, trace.WithAttributes(attrs...))
}

// StartServerSpan creates a new server-kind span for incoming HTTP requests.
func StartServerSpan(ctx context.Context, name string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	return Tracer().Start(ctx, name, trace.WithAttributes(attrs...), trace.WithSpanKind(trace.SpanKindServer))
}

// StartClientSpan creates a new client-kind span for outgoing Gmail API calls.
func StartClientSpan(ctx context.Context, name string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	return Tracer().Start(ctx, name, trace.WithAttributes(attrs...), trace.WithSpanKind(trace.SpanKindClient))
}

// -------------------------------------------------------------------------
// ATTRIBUTE HELPERS
// -------------------------------------------------------------------------

// RequestAttributes builds common attributes for an incoming S3 request.
func RequestAttributes(method, path, bucket, key, clientIP string) []attribute.KeyValue {
	return []attribute.KeyValue{
		semconv.HTTPRequestMethodKey.String(method),
		semconv.URLPath(path),
		AttrBucket.String(bucket),
		AttrObjectKey.String(key),
		semconv.ClientAddress(clientIP),
	}
}

// GmailAttributes builds attributes for a Gmail API operation.
func GmailAttributes(operation, bucket, key string) []attribute.KeyValue {
	return []attribute.KeyValue{
		AttrOperation.String(operation),
		AttrBucket.String(bucket),
		AttrObjectKey.String(key),
	}
}

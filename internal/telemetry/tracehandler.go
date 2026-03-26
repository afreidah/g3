// -------------------------------------------------------------------------------
// Trace Handler - slog Handler for Log/Span Correlation
//
// Author: Alex Freidah
//
// Wraps an inner slog.Handler to inject OpenTelemetry trace_id and span_id
// attributes into every log record that has an active span in context. Enables
// correlation between structured logs and distributed traces in backends like
// Grafana Loki + Tempo.
// -------------------------------------------------------------------------------

package telemetry

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/trace"
)

// -------------------------------------------------------------------------
// TRACE HANDLER
// -------------------------------------------------------------------------

// TraceHandler wraps an inner slog.Handler to inject trace context fields.
type TraceHandler struct {
	inner slog.Handler
}

// NewTraceHandler creates a TraceHandler that decorates log records with
// trace_id and span_id when an active OpenTelemetry span exists in context.
func NewTraceHandler(inner slog.Handler) *TraceHandler {
	return &TraceHandler{inner: inner}
}

// Enabled delegates to the inner handler.
func (h *TraceHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

// Handle injects trace_id and span_id into the record if the context carries
// a valid span, then delegates to the inner handler.
func (h *TraceHandler) Handle(ctx context.Context, r slog.Record) error {
	span := trace.SpanFromContext(ctx)
	if span.SpanContext().IsValid() {
		r.AddAttrs(
			slog.String("trace_id", span.SpanContext().TraceID().String()),
			slog.String("span_id", span.SpanContext().SpanID().String()),
		)
	}
	return h.inner.Handle(ctx, r)
}

// WithAttrs returns a new TraceHandler wrapping the inner handler with
// additional attributes.
func (h *TraceHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &TraceHandler{inner: h.inner.WithAttrs(attrs)}
}

// WithGroup returns a new TraceHandler wrapping the inner handler with
// a group prefix.
func (h *TraceHandler) WithGroup(name string) slog.Handler {
	return &TraceHandler{inner: h.inner.WithGroup(name)}
}

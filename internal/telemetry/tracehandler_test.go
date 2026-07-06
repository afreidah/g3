// -------------------------------------------------------------------------------
// Trace Handler Tests
//
// Author: Alex Freidah
//
// Verifies that TraceHandler injects trace_id/span_id when the context carries
// a valid span and leaves records untouched otherwise, and that the decorator
// methods delegate to the inner handler.
// -------------------------------------------------------------------------------

package telemetry

import (
	"context"
	"log/slog"
	"testing"

	"go.opentelemetry.io/otel/trace"
)

// capturingHandler records the last slog.Record it handled for assertions.
type capturingHandler struct {
	rec     slog.Record
	handled bool
	attrs   []slog.Attr
	group   string
}

func (c *capturingHandler) Enabled(context.Context, slog.Level) bool { return true }
func (c *capturingHandler) Handle(_ context.Context, r slog.Record) error {
	c.rec = r
	c.handled = true
	return nil
}
func (c *capturingHandler) WithAttrs(a []slog.Attr) slog.Handler { c.attrs = a; return c }
func (c *capturingHandler) WithGroup(n string) slog.Handler      { c.group = n; return c }

func attrValue(r slog.Record, key string) (string, bool) {
	var out string
	var found bool
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == key {
			out = a.Value.String()
			found = true
			return false
		}
		return true
	})
	return out, found
}

func TestTraceHandler_NoSpan(t *testing.T) {
	inner := &capturingHandler{}
	h := NewTraceHandler(inner)

	if !h.Enabled(context.Background(), slog.LevelInfo) {
		t.Fatal("Enabled = false, want true")
	}

	rec := slog.Record{Message: "msg"}
	if err := h.Handle(context.Background(), rec); err != nil {
		t.Fatalf("Handle err = %v", err)
	}
	if _, ok := attrValue(inner.rec, "trace_id"); ok {
		t.Error("trace_id injected without an active span")
	}
}

func TestTraceHandler_WithSpan(t *testing.T) {
	inner := &capturingHandler{}
	h := NewTraceHandler(inner)

	traceID, _ := trace.TraceIDFromHex("0123456789abcdef0123456789abcdef")
	spanID, _ := trace.SpanIDFromHex("0123456789abcdef")
	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: trace.FlagsSampled,
	})
	ctx := trace.ContextWithSpanContext(context.Background(), sc)

	if err := h.Handle(ctx, slog.Record{Message: "msg"}); err != nil {
		t.Fatalf("Handle err = %v", err)
	}

	gotTrace, ok := attrValue(inner.rec, "trace_id")
	if !ok || gotTrace != traceID.String() {
		t.Errorf("trace_id = %q (present=%v), want %q", gotTrace, ok, traceID.String())
	}
	gotSpan, ok := attrValue(inner.rec, "span_id")
	if !ok || gotSpan != spanID.String() {
		t.Errorf("span_id = %q (present=%v), want %q", gotSpan, ok, spanID.String())
	}
}

func TestTraceHandler_Delegation(t *testing.T) {
	inner := &capturingHandler{}
	h := NewTraceHandler(inner)

	h.WithAttrs([]slog.Attr{slog.String("k", "v")})
	if len(inner.attrs) != 1 || inner.attrs[0].Key != "k" {
		t.Errorf("WithAttrs did not delegate: %v", inner.attrs)
	}

	h.WithGroup("grp")
	if inner.group != "grp" {
		t.Errorf("WithGroup did not delegate: %q", inner.group)
	}
}

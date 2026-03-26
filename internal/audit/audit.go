// -------------------------------------------------------------------------------
// Audit - Request ID Generation and Structured Audit Logging
//
// Author: Alex Freidah
//
// Provides context-based request ID propagation and structured audit logging for
// security-relevant operations. Audit entries are distinguished by the "audit":
// true field in JSON log output and include a request_id for correlation across
// the request lifecycle.
// -------------------------------------------------------------------------------

package audit

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
)

// -------------------------------------------------------------------------
// TYPES
// -------------------------------------------------------------------------

type contextKey int

const requestIDKey contextKey = iota

// OnEvent is an optional callback invoked on each audit event for metrics
// integration. Set this to a function that increments a Prometheus counter.
var OnEvent func(event string)

// -------------------------------------------------------------------------
// REQUEST ID
// -------------------------------------------------------------------------

// NewID generates a random 16-byte request ID encoded as 32 hex characters.
func NewID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// WithRequestID returns a new context carrying the given request ID.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}

// RequestID extracts the request ID from the context, or returns an empty
// string if none is set.
func RequestID(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDKey).(string); ok {
		return id
	}
	return ""
}

// -------------------------------------------------------------------------
// AUDIT LOGGING
// -------------------------------------------------------------------------

// Log emits a structured audit log entry at INFO level. The entry includes
// the audit marker, event name, and request ID from context. Additional
// attributes are appended after the base fields.
func Log(ctx context.Context, event string, attrs ...slog.Attr) {
	if OnEvent != nil {
		OnEvent(event)
	}

	base := []slog.Attr{
		slog.Bool("audit", true),
		slog.String("event", event),
	}

	if id := RequestID(ctx); id != "" {
		base = append(base, slog.String("request_id", id))
	}

	base = append(base, attrs...)
	slog.LogAttrs(ctx, slog.LevelInfo, "audit", base...)
}

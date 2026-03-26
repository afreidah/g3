// -------------------------------------------------------------------------------
// Log Buffer - Circular Buffer and Tee Handler for Recent Log Access
//
// Author: Alex Freidah
//
// Provides a thread-safe circular buffer that stores recent log entries for
// operational visibility. The TeeHandler fans log output to both the primary
// handler (JSON to stdout) and the buffer, enabling dashboards or admin
// endpoints to display recent logs without external log aggregation.
// -------------------------------------------------------------------------------

package telemetry

import (
	"context"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"
)

const logBufferCapacity = 5000

// -------------------------------------------------------------------------
// LOG ENTRY
// -------------------------------------------------------------------------

// LogEntry represents a single buffered log record with extracted attributes.
type LogEntry struct {
	Time    time.Time      `json:"time"`
	Level   string         `json:"level"`
	Message string         `json:"message"`
	Attrs   map[string]any `json:"attrs,omitempty"`
}

// -------------------------------------------------------------------------
// LOG BUFFER
// -------------------------------------------------------------------------

// LogBuffer is a thread-safe circular buffer of recent log entries.
type LogBuffer struct {
	mu      sync.RWMutex
	entries []LogEntry
	pos     int
	full    bool
}

// NewLogBuffer creates a LogBuffer with a fixed capacity of 5000 entries.
func NewLogBuffer() *LogBuffer {
	return &LogBuffer{
		entries: make([]LogEntry, logBufferCapacity),
	}
}

// Add appends a log entry to the buffer, overwriting the oldest entry when
// the buffer is full.
func (b *LogBuffer) Add(entry LogEntry) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.entries[b.pos] = entry
	b.pos++
	if b.pos >= len(b.entries) {
		b.pos = 0
		b.full = true
	}
}

// Entries returns buffered log entries in chronological order, limited to
// the most recent count entries. Pass 0 for all buffered entries.
func (b *LogBuffer) Entries(count int) []LogEntry {
	b.mu.RLock()
	defer b.mu.RUnlock()

	var result []LogEntry
	if b.full {
		result = make([]LogEntry, logBufferCapacity)
		copy(result, b.entries[b.pos:])
		copy(result[logBufferCapacity-b.pos:], b.entries[:b.pos])
	} else {
		result = make([]LogEntry, b.pos)
		copy(result, b.entries[:b.pos])
	}

	if count > 0 && count < len(result) {
		result = result[len(result)-count:]
	}
	return result
}

// -------------------------------------------------------------------------
// TEE HANDLER
// -------------------------------------------------------------------------

// TeeHandler fans slog output to both a primary handler and a LogBuffer.
type TeeHandler struct {
	primary slog.Handler
	buf     *LogBuffer
	attrs   []slog.Attr
	groups  []string
}

// NewTeeHandler creates a handler that writes to both the primary handler
// and the log buffer.
func NewTeeHandler(primary slog.Handler, buf *LogBuffer) *TeeHandler {
	return &TeeHandler{primary: primary, buf: buf}
}

// Enabled delegates to the primary handler.
func (h *TeeHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.primary.Enabled(ctx, level)
}

// Handle writes to both the primary handler and the log buffer.
func (h *TeeHandler) Handle(ctx context.Context, r slog.Record) error {
	err := h.primary.Handle(ctx, r)

	entry := LogEntry{
		Time:    r.Time,
		Level:   r.Level.String(),
		Message: r.Message,
	}

	attrs := make(map[string]any)
	prefix := groupPrefix(h.groups)
	for _, a := range h.attrs {
		attrs[prefix+a.Key] = a.Value.Any()
	}
	r.Attrs(func(a slog.Attr) bool {
		attrs[prefix+a.Key] = a.Value.Any()
		return true
	})

	if len(attrs) > 0 {
		entry.Attrs = attrs
	}
	h.buf.Add(entry)
	return err
}

// WithAttrs returns a new TeeHandler with additional attributes.
func (h *TeeHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &TeeHandler{
		primary: h.primary.WithAttrs(attrs),
		buf:     h.buf,
		attrs:   append(slices.Clone(h.attrs), attrs...),
		groups:  slices.Clone(h.groups),
	}
}

// WithGroup returns a new TeeHandler with a group prefix.
func (h *TeeHandler) WithGroup(name string) slog.Handler {
	return &TeeHandler{
		primary: h.primary.WithGroup(name),
		buf:     h.buf,
		attrs:   slices.Clone(h.attrs),
		groups:  append(slices.Clone(h.groups), name),
	}
}

// groupPrefix builds a dotted prefix from group names.
func groupPrefix(groups []string) string {
	if len(groups) == 0 {
		return ""
	}
	return strings.Join(groups, ".") + "."
}

// -------------------------------------------------------------------------------
// Log Buffer Tests
//
// Author: Alex Freidah
//
// Exercises the circular LogBuffer (partial, wraparound, and count-limited
// reads) and the TeeHandler fan-out to a primary handler plus the buffer,
// including attribute and group propagation.
// -------------------------------------------------------------------------------

package telemetry

import (
	"bytes"
	"context"
	"log/slog"
	"testing"
	"time"
)

func TestLogBuffer_Partial(t *testing.T) {
	b := NewLogBuffer()
	b.Add(LogEntry{Message: "a"})
	b.Add(LogEntry{Message: "b"})

	got := b.Entries(0)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].Message != "a" || got[1].Message != "b" {
		t.Errorf("order = %q, %q, want a, b", got[0].Message, got[1].Message)
	}
}

func TestLogBuffer_CountLimit(t *testing.T) {
	b := NewLogBuffer()
	for _, m := range []string{"a", "b", "c"} {
		b.Add(LogEntry{Message: m})
	}
	got := b.Entries(2)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].Message != "b" || got[1].Message != "c" {
		t.Errorf("got %q, %q, want b, c (most recent)", got[0].Message, got[1].Message)
	}
}

func TestLogBuffer_Wraparound(t *testing.T) {
	b := NewLogBuffer()
	// Overfill by two so the oldest two entries are overwritten.
	total := logBufferCapacity + 2
	for i := range total {
		b.Add(LogEntry{Message: string(rune('A' + i%26)), Time: time.Unix(int64(i), 0)})
	}

	got := b.Entries(0)
	if len(got) != logBufferCapacity {
		t.Fatalf("len = %d, want %d", len(got), logBufferCapacity)
	}
	// Oldest surviving entry is index 2 (the first two were overwritten), and
	// chronological order must be preserved across the wrap point.
	if !got[0].Time.Equal(time.Unix(2, 0)) {
		t.Errorf("oldest time = %v, want %v", got[0].Time, time.Unix(2, 0))
	}
	last := got[len(got)-1]
	if !last.Time.Equal(time.Unix(int64(total-1), 0)) {
		t.Errorf("newest time = %v, want %v", last.Time, time.Unix(int64(total-1), 0))
	}

	// Count limit on a full buffer returns the most recent N.
	tail := b.Entries(3)
	if len(tail) != 3 || !tail[2].Time.Equal(time.Unix(int64(total-1), 0)) {
		t.Errorf("tail newest = %v, want %v", tail[len(tail)-1].Time, time.Unix(int64(total-1), 0))
	}
}

func TestTeeHandler_Handle(t *testing.T) {
	var buf bytes.Buffer
	primary := slog.NewJSONHandler(&buf, nil)
	lb := NewLogBuffer()
	h := NewTeeHandler(primary, lb)

	if !h.Enabled(context.Background(), slog.LevelInfo) {
		t.Fatal("Enabled = false, want true for Info on default handler")
	}

	logger := slog.New(h)
	logger.InfoContext(context.Background(), "hello", "key", "val")

	// Primary handler received the record.
	if buf.Len() == 0 {
		t.Error("primary handler wrote nothing")
	}

	entries := lb.Entries(0)
	if len(entries) != 1 {
		t.Fatalf("buffered %d entries, want 1", len(entries))
	}
	e := entries[0]
	if e.Message != "hello" || e.Level != "INFO" {
		t.Errorf("entry = %+v, want message=hello level=INFO", e)
	}
	if e.Attrs["key"] != "val" {
		t.Errorf("attrs = %v, want key=val", e.Attrs)
	}
}

func TestTeeHandler_WithAttrsAndGroup(t *testing.T) {
	lb := NewLogBuffer()
	h := NewTeeHandler(slog.NewJSONHandler(&bytes.Buffer{}, nil), lb)

	logger := slog.New(h).WithGroup("req").With("rid", "123")
	logger.InfoContext(context.Background(), "done", "status", 200)

	entries := lb.Entries(0)
	if len(entries) != 1 {
		t.Fatalf("buffered %d entries, want 1", len(entries))
	}
	attrs := entries[0].Attrs
	if attrs["req.rid"] != "123" {
		t.Errorf("attrs = %v, want req.rid=123", attrs)
	}
	if attrs["req.status"] != int64(200) {
		t.Errorf("attrs = %v, want req.status=200", attrs)
	}
}

func TestGroupPrefix(t *testing.T) {
	if got := groupPrefix(nil); got != "" {
		t.Errorf("groupPrefix(nil) = %q, want empty", got)
	}
	if got := groupPrefix([]string{"a", "b"}); got != "a.b." {
		t.Errorf("groupPrefix = %q, want a.b.", got)
	}
}

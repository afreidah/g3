// -------------------------------------------------------------------------------
// Audit Tests
//
// Author: Alex Freidah
//
// Unit tests for request ID generation, context propagation, and the OnEvent
// callback mechanism.
// -------------------------------------------------------------------------------

package audit

import (
	"context"
	"testing"
)

// -------------------------------------------------------------------------
// REQUEST ID
// -------------------------------------------------------------------------

func TestNewID_Length(t *testing.T) {
	id := NewID()
	if len(id) != 32 {
		t.Errorf("NewID() length = %d, want 32", len(id))
	}
}

func TestNewID_Unique(t *testing.T) {
	ids := make(map[string]bool)
	for range 1000 {
		id := NewID()
		if ids[id] {
			t.Fatalf("duplicate ID: %s", id)
		}
		ids[id] = true
	}
}

// -------------------------------------------------------------------------
// CONTEXT PROPAGATION
// -------------------------------------------------------------------------

func TestWithRequestID_RoundTrip(t *testing.T) {
	ctx := context.Background()
	id := "abcdef1234567890abcdef1234567890"

	ctx = WithRequestID(ctx, id)
	got := RequestID(ctx)
	if got != id {
		t.Errorf("RequestID = %q, want %q", got, id)
	}
}

func TestRequestID_EmptyContext(t *testing.T) {
	got := RequestID(context.Background())
	if got != "" {
		t.Errorf("RequestID on empty context = %q, want empty", got)
	}
}

// -------------------------------------------------------------------------
// ON EVENT CALLBACK
// -------------------------------------------------------------------------

func TestOnEvent_Called(t *testing.T) {
	var called string
	original := OnEvent
	OnEvent = func(event string) {
		called = event
	}
	defer func() { OnEvent = original }()

	Log(context.Background(), "test.event")
	if called != "test.event" {
		t.Errorf("OnEvent called with %q, want %q", called, "test.event")
	}
}

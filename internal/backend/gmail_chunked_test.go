// -------------------------------------------------------------------------------
// Chunked Object Tests - Chunk Index Parsing
//
// Author: Alex Freidah
//
// Unit tests for chunk index extraction from email subjects and raw email
// headers.
// -------------------------------------------------------------------------------

package backend

import (
	"testing"
)

// -------------------------------------------------------------------------
// CHUNK INDEX PARSING
// -------------------------------------------------------------------------

func TestParseChunkIndex(t *testing.T) {
	tests := []struct {
		name    string
		subject string
		want    int
	}{
		{"normal", "s3://bucket/key#chunk-003", 3},
		{"first", "s3://bucket/key#chunk-001", 1},
		{"large", "s3://bucket/key#chunk-999", 999},
		{"no chunk marker", "s3://bucket/key", 0},
		{"empty", "", 0},
		{"partial marker", "s3://bucket/key#chunk-", 0},
		{"non-numeric", "s3://bucket/key#chunk-abc", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseChunkIndex(tt.subject)
			if got != tt.want {
				t.Errorf("parseChunkIndex(%q) = %d, want %d", tt.subject, got, tt.want)
			}
		})
	}
}

func TestParseChunkIndexFromRaw(t *testing.T) {
	raw := []byte("From: me\r\nTo: me\r\nSubject: s3://bucket/key#chunk-005\r\nMIME-Version: 1.0\r\n\r\nbody")
	got := parseChunkIndexFromRaw(raw, "bucket", "key")
	if got != 5 {
		t.Errorf("parseChunkIndexFromRaw = %d, want 5", got)
	}
}

func TestParseChunkIndexFromRaw_NoSubject(t *testing.T) {
	raw := []byte("From: me\r\nTo: me\r\n\r\nbody")
	got := parseChunkIndexFromRaw(raw, "bucket", "key")
	if got != 0 {
		t.Errorf("parseChunkIndexFromRaw = %d, want 0", got)
	}
}

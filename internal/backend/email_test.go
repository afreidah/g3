// -------------------------------------------------------------------------------
// Email Construction Tests
//
// Author: Alex Freidah
//
// Unit tests for MIME email building and parsing. Verifies round-trip
// correctness: building an email and parsing it back should yield the
// original metadata and attachment data.
// -------------------------------------------------------------------------------

package backend

import (
	"bytes"
	"testing"
	"time"
)

// -------------------------------------------------------------------------
// ROUND TRIP
// -------------------------------------------------------------------------

func TestBuildAndParseObjectEmail_RoundTrip(t *testing.T) {
	meta := &objectMetadata{
		ContentType: "text/plain",
		ETag:        "abc123",
		Size:        11,
		Metadata:    map[string]string{"author": "alex"},
		CreatedAt:   time.Date(2026, 3, 26, 12, 0, 0, 0, time.UTC),
	}
	data := []byte("hello world")

	raw, err := buildObjectEmail("s3://test/key.txt", meta, data)
	if err != nil {
		t.Fatalf("buildObjectEmail: %v", err)
	}

	parsedMeta, parsedData, err := parseObjectEmail(raw)
	if err != nil {
		t.Fatalf("parseObjectEmail: %v", err)
	}

	if parsedMeta.ContentType != meta.ContentType {
		t.Errorf("ContentType = %q, want %q", parsedMeta.ContentType, meta.ContentType)
	}
	if parsedMeta.ETag != meta.ETag {
		t.Errorf("ETag = %q, want %q", parsedMeta.ETag, meta.ETag)
	}
	if parsedMeta.Size != meta.Size {
		t.Errorf("Size = %d, want %d", parsedMeta.Size, meta.Size)
	}
	if parsedMeta.Metadata["author"] != "alex" {
		t.Errorf("Metadata[author] = %q, want %q", parsedMeta.Metadata["author"], "alex")
	}
	if !bytes.Equal(parsedData, data) {
		t.Errorf("data = %q, want %q", parsedData, data)
	}
}

func TestBuildAndParseObjectEmail_NoAttachment(t *testing.T) {
	meta := &objectMetadata{
		ContentType: "application/json",
		ETag:        "manifest123",
		Size:        1024,
		Chunked:     true,
		ChunkCount:  3,
		TotalSize:   1024,
		CreatedAt:   time.Now().UTC(),
	}

	raw, err := buildObjectEmail("s3://test/bigfile", meta, nil)
	if err != nil {
		t.Fatalf("buildObjectEmail: %v", err)
	}

	parsedMeta, parsedData, err := parseObjectEmail(raw)
	if err != nil {
		t.Fatalf("parseObjectEmail: %v", err)
	}

	if !parsedMeta.Chunked {
		t.Error("expected Chunked = true")
	}
	if parsedMeta.ChunkCount != 3 {
		t.Errorf("ChunkCount = %d, want 3", parsedMeta.ChunkCount)
	}
	if len(parsedData) != 0 {
		t.Errorf("expected empty attachment, got %d bytes", len(parsedData))
	}
}

// -------------------------------------------------------------------------
// BINARY DATA
// -------------------------------------------------------------------------

func TestBuildAndParseObjectEmail_BinaryData(t *testing.T) {
	// Generate binary data with all byte values
	data := make([]byte, 256)
	for i := range data {
		data[i] = byte(i)
	}

	meta := &objectMetadata{
		ContentType: "application/octet-stream",
		ETag:        "binarytest",
		Size:        int64(len(data)),
		CreatedAt:   time.Now().UTC(),
	}

	raw, err := buildObjectEmail("s3://test/binary.bin", meta, data)
	if err != nil {
		t.Fatalf("buildObjectEmail: %v", err)
	}

	_, parsedData, err := parseObjectEmail(raw)
	if err != nil {
		t.Fatalf("parseObjectEmail: %v", err)
	}

	if !bytes.Equal(parsedData, data) {
		t.Errorf("binary round-trip failed: got %d bytes, want %d", len(parsedData), len(data))
	}
}

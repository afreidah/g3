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
	"strings"
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

// -------------------------------------------------------------------------
// PARSE METADATA FOR SYNC
// -------------------------------------------------------------------------

func TestParseMetadataForSync_Success(t *testing.T) {
	body := `{"content_type":"text/plain","etag":"abc123","size":42,"drive_file_id":"drv456","created_at":"2026-03-27T00:00:00Z","metadata":{"author":"alex"}}`

	meta, err := ParseMetadataForSync(body)
	if err != nil {
		t.Fatalf("ParseMetadataForSync: %v", err)
	}
	if meta.ContentType != "text/plain" {
		t.Errorf("ContentType = %q, want %q", meta.ContentType, "text/plain")
	}
	if meta.ETag != "abc123" {
		t.Errorf("ETag = %q, want %q", meta.ETag, "abc123")
	}
	if meta.Size != 42 {
		t.Errorf("Size = %d, want 42", meta.Size)
	}
	if meta.DriveFileID != "drv456" {
		t.Errorf("DriveFileID = %q, want %q", meta.DriveFileID, "drv456")
	}
	if meta.Metadata["author"] != "alex" {
		t.Errorf("Metadata[author] = %q, want %q", meta.Metadata["author"], "alex")
	}
}

func TestParseMetadataForSync_NoDriveFileID(t *testing.T) {
	body := `{"content_type":"application/octet-stream","etag":"def","size":100,"created_at":"2026-01-01T00:00:00Z"}`

	meta, err := ParseMetadataForSync(body)
	if err != nil {
		t.Fatalf("ParseMetadataForSync: %v", err)
	}
	if meta.DriveFileID != "" {
		t.Errorf("DriveFileID = %q, want empty", meta.DriveFileID)
	}
	if meta.Size != 100 {
		t.Errorf("Size = %d, want 100", meta.Size)
	}
}

func TestParseMetadataForSync_InvalidJSON(t *testing.T) {
	_, err := ParseMetadataForSync("{invalid")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestParseMetadataForSync_EmptyBody(t *testing.T) {
	_, err := ParseMetadataForSync("")
	if err == nil {
		t.Fatal("expected error for empty body")
	}
}

// -------------------------------------------------------------------------
// HEADER / BODY SPLITTING
// -------------------------------------------------------------------------

func TestSplitHeadersBody(t *testing.T) {
	t.Run("CRLF separator", func(t *testing.T) {
		hdr, body, err := splitHeadersBody([]byte("A: b\r\n\r\nBODY"))
		if err != nil || hdr != "A: b" || string(body) != "BODY" {
			t.Fatalf("hdr=%q body=%q err=%v", hdr, body, err)
		}
	})
	t.Run("LF separator", func(t *testing.T) {
		hdr, body, err := splitHeadersBody([]byte("A: b\n\nBODY"))
		if err != nil || hdr != "A: b" || string(body) != "BODY" {
			t.Fatalf("hdr=%q body=%q err=%v", hdr, body, err)
		}
	})
	t.Run("no separator", func(t *testing.T) {
		if _, _, err := splitHeadersBody([]byte("no separator here")); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestFirstContentTypeHeader(t *testing.T) {
	got := firstContentTypeHeader("Subject: x\nContent-Type: text/plain; charset=utf-8\nDate: y")
	if got != "text/plain; charset=utf-8" {
		t.Errorf("got %q", got)
	}
	if v := firstContentTypeHeader("Subject: x\nDate: y"); v != "" {
		t.Errorf("expected empty, got %q", v)
	}
}

// -------------------------------------------------------------------------
// PARSE OBJECT EMAIL BRANCHES
// -------------------------------------------------------------------------

// multipartEmail assembles a multipart/mixed message from the given part blocks
// (each "headers\r\n\r\nbody") under a fixed boundary.
func multipartEmail(parts ...string) []byte {
	var b strings.Builder
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: multipart/mixed; boundary=\"B\"\r\n\r\n")
	for _, p := range parts {
		b.WriteString("--B\r\n")
		b.WriteString(p)
		b.WriteString("\r\n")
	}
	b.WriteString("--B--\r\n")
	return []byte(b.String())
}

const (
	validMetaPart   = "Content-Type: text/plain; charset=utf-8\r\n\r\n" + `{"content_type":"text/plain","etag":"e","size":7}`
	plainAttachPart = "Content-Type: application/octet-stream\r\n" +
		"Content-Disposition: attachment; filename=\"object.bin\"\r\n\r\nRAWDATA"
)

func TestParseObjectEmail_PlainTextMetadataOnly(t *testing.T) {
	raw := []byte("Content-Type: text/plain; charset=utf-8\r\n\r\n" + `{"content_type":"text/plain","etag":"e","size":5}`)
	meta, data, err := parseObjectEmail(raw)
	if err != nil {
		t.Fatalf("parseObjectEmail: %v", err)
	}
	if meta == nil || meta.ETag != "e" {
		t.Errorf("meta = %+v", meta)
	}
	if data != nil {
		t.Errorf("expected nil data, got %q", data)
	}
}

func TestParseObjectEmail_UnencodedAttachment(t *testing.T) {
	meta, data, err := parseObjectEmail(multipartEmail(validMetaPart, plainAttachPart))
	if err != nil {
		t.Fatalf("parseObjectEmail: %v", err)
	}
	if meta == nil || meta.ETag != "e" {
		t.Errorf("meta = %+v", meta)
	}
	if string(data) != "RAWDATA" {
		t.Errorf("data = %q, want %q", data, "RAWDATA")
	}
}

func TestParseObjectEmail_Errors(t *testing.T) {
	badMetaPart := "Content-Type: text/plain; charset=utf-8\r\n\r\n{invalid"
	tests := []struct {
		name string
		raw  []byte
	}{
		{"no separator", []byte("garbage with no header body separator")},
		{"bad content type", []byte("Content-Type: @@@\r\n\r\nbody")},
		{"no boundary", []byte("Content-Type: multipart/mixed\r\n\r\nbody")},
		{"no metadata part", multipartEmail(plainAttachPart)},
		{"bad metadata json", multipartEmail(badMetaPart, plainAttachPart)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, _, err := parseObjectEmail(tt.raw); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

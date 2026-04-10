// -------------------------------------------------------------------------------
// Multipart Store Tests
//
// Author: Alex Freidah
//
// Unit tests for the in-memory multipart upload store covering create, add
// part, complete, abort, and TTL-based expiry.
// -------------------------------------------------------------------------------

package server

import (
	"io"
	"testing"
	"time"
)

// -------------------------------------------------------------------------
// CREATE AND COMPLETE
// -------------------------------------------------------------------------

func TestMultipartStore_CreateAndComplete(t *testing.T) {
	store := NewMultipartStore(1 * time.Hour)

	uploadID := store.create("bucket", "key.txt", "text/plain", nil)
	if uploadID == "" {
		t.Fatal("expected non-empty upload ID")
	}

	_, err := store.addPart(uploadID, 1, []byte("hello "))
	if err != nil {
		t.Fatalf("addPart 1: %v", err)
	}

	_, err = store.addPart(uploadID, 2, []byte("world"))
	if err != nil {
		t.Fatalf("addPart 2: %v", err)
	}

	upload, reader, size, err := store.complete(uploadID)
	if err != nil {
		t.Fatalf("complete: %v", err)
	}

	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read assembled data: %v", err)
	}

	if upload.bucket != "bucket" {
		t.Errorf("bucket = %q, want %q", upload.bucket, "bucket")
	}
	if upload.key != "key.txt" {
		t.Errorf("key = %q, want %q", upload.key, "key.txt")
	}
	if string(data) != "hello world" {
		t.Errorf("data = %q, want %q", string(data), "hello world")
	}
	if size != int64(len(data)) {
		t.Errorf("size = %d, want %d", size, len(data))
	}
}

func TestMultipartStore_PartsOrderedByNumber(t *testing.T) {
	store := NewMultipartStore(1 * time.Hour)
	uploadID := store.create("b", "k", "application/octet-stream", nil)

	// Add parts out of order
	_, _ = store.addPart(uploadID, 3, []byte("c"))
	_, _ = store.addPart(uploadID, 1, []byte("a"))
	_, _ = store.addPart(uploadID, 2, []byte("b"))

	_, reader, size, err := store.complete(uploadID)
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read assembled data: %v", err)
	}
	if string(data) != "abc" {
		t.Errorf("data = %q, want %q", string(data), "abc")
	}
	if size != 3 {
		t.Errorf("size = %d, want 3", size)
	}
}

// -------------------------------------------------------------------------
// ETAG
// -------------------------------------------------------------------------

func TestMultipartStore_AddPartReturnsETag(t *testing.T) {
	store := NewMultipartStore(1 * time.Hour)
	uploadID := store.create("b", "k", "text/plain", nil)

	etag, err := store.addPart(uploadID, 1, []byte("test"))
	if err != nil {
		t.Fatalf("addPart: %v", err)
	}
	if etag == "" {
		t.Error("expected non-empty ETag")
	}
	if len(etag) != 32 {
		t.Errorf("ETag length = %d, want 32 (MD5 hex)", len(etag))
	}
}

// -------------------------------------------------------------------------
// METADATA
// -------------------------------------------------------------------------

func TestMultipartStore_PreservesMetadata(t *testing.T) {
	store := NewMultipartStore(1 * time.Hour)
	meta := map[string]string{"author": "alex"}
	uploadID := store.create("b", "k", "image/png", meta)

	_, _ = store.addPart(uploadID, 1, []byte("data"))
	upload, _, _, err := store.complete(uploadID)
	if err != nil {
		t.Fatalf("complete: %v", err)
	}

	if upload.contentType != "image/png" {
		t.Errorf("contentType = %q, want %q", upload.contentType, "image/png")
	}
	if upload.metadata["author"] != "alex" {
		t.Errorf("metadata[author] = %q, want %q", upload.metadata["author"], "alex")
	}
}

// -------------------------------------------------------------------------
// ERROR CASES
// -------------------------------------------------------------------------

func TestMultipartStore_PartNumberTooLow(t *testing.T) {
	store := NewMultipartStore(1 * time.Hour)
	uploadID := store.create("b", "k", "text/plain", nil)

	_, err := store.addPart(uploadID, 0, []byte("data"))
	if err == nil {
		t.Fatal("expected error for part number 0")
	}
}

func TestMultipartStore_PartNumberTooHigh(t *testing.T) {
	store := NewMultipartStore(1 * time.Hour)
	uploadID := store.create("b", "k", "text/plain", nil)

	_, err := store.addPart(uploadID, 10001, []byte("data"))
	if err == nil {
		t.Fatal("expected error for part number > 10000")
	}
}

func TestMultipartStore_MaxConcurrentUploads(t *testing.T) {
	store := NewMultipartStore(1 * time.Hour)

	for range maxConcurrentUploads {
		id := store.create("b", "k", "text/plain", nil)
		if id == "" {
			t.Fatal("expected upload to succeed within limit")
		}
	}

	id := store.create("b", "k", "text/plain", nil)
	if id != "" {
		t.Fatal("expected empty upload ID when limit exceeded")
	}
}

func TestMultipartStore_AddPartUnknownUpload(t *testing.T) {
	store := NewMultipartStore(1 * time.Hour)

	_, err := store.addPart("nonexistent", 1, []byte("data"))
	if err == nil {
		t.Fatal("expected error for unknown upload ID")
	}
}

func TestMultipartStore_CompleteUnknownUpload(t *testing.T) {
	store := NewMultipartStore(1 * time.Hour)

	_, _, _, err := store.complete("nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown upload ID")
	}
}

func TestMultipartStore_CompleteTwiceFails(t *testing.T) {
	store := NewMultipartStore(1 * time.Hour)
	uploadID := store.create("b", "k", "text/plain", nil)
	_, _ = store.addPart(uploadID, 1, []byte("data"))

	_, _, _, err := store.complete(uploadID)
	if err != nil {
		t.Fatalf("first complete: %v", err)
	}

	_, _, _, err = store.complete(uploadID)
	if err == nil {
		t.Fatal("expected error on second complete")
	}
}

// -------------------------------------------------------------------------
// ABORT
// -------------------------------------------------------------------------

func TestMultipartStore_Abort(t *testing.T) {
	store := NewMultipartStore(1 * time.Hour)
	uploadID := store.create("b", "k", "text/plain", nil)
	_, _ = store.addPart(uploadID, 1, []byte("data"))

	store.abort(uploadID)

	_, _, _, err := store.complete(uploadID)
	if err == nil {
		t.Fatal("expected error after abort")
	}
}

func TestMultipartStore_AbortNonexistent(t *testing.T) {
	store := NewMultipartStore(1 * time.Hour)
	// Should not panic
	store.abort("nonexistent")
}

// -------------------------------------------------------------------------
// EXPIRY
// -------------------------------------------------------------------------

func TestMultipartStore_CleanExpired(t *testing.T) {
	store := NewMultipartStore(1 * time.Millisecond)
	store.create("b", "k1", "text/plain", nil)
	store.create("b", "k2", "text/plain", nil)

	time.Sleep(5 * time.Millisecond)

	removed := store.CleanExpired()
	if removed != 2 {
		t.Errorf("removed = %d, want 2", removed)
	}

	// Verify they're gone
	store.mu.RLock()
	count := len(store.uploads)
	store.mu.RUnlock()
	if count != 0 {
		t.Errorf("remaining uploads = %d, want 0", count)
	}
}

func TestMultipartStore_CleanExpiredKeepsFresh(t *testing.T) {
	store := NewMultipartStore(1 * time.Hour)
	store.create("b", "fresh", "text/plain", nil)

	removed := store.CleanExpired()
	if removed != 0 {
		t.Errorf("removed = %d, want 0", removed)
	}
}

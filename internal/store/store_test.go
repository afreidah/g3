// -------------------------------------------------------------------------------
// Store Tests - SQLite Metadata Index
//
// Author: Alex Freidah
//
// Unit tests for the SQLite metadata store covering object and bucket CRUD,
// listing with prefix filtering, and pagination.
// -------------------------------------------------------------------------------

package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/afreidah/g3/internal/backend"
)

// -------------------------------------------------------------------------
// HELPERS
// -------------------------------------------------------------------------

func newTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	s, err := NewSQLite(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// -------------------------------------------------------------------------
// OBJECT CRUD
// -------------------------------------------------------------------------

func TestStore_PutAndGetObject(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	rec := &backend.ObjectRecord{
		Bucket:      "test",
		Key:         "photos/cat.jpg",
		GmailMsgID:  "msg123",
		ETag:        "abc",
		Size:        1024,
		ContentType: "image/jpeg",
		CreatedAt:   time.Date(2026, 3, 27, 0, 0, 0, 0, time.UTC),
		Metadata:    map[string]string{"author": "alex"},
	}

	if err := s.PutObject(ctx, rec); err != nil {
		t.Fatalf("PutObject: %v", err)
	}

	got, err := s.GetObject(ctx, "test", "photos/cat.jpg")
	if err != nil {
		t.Fatalf("GetObject: %v", err)
	}
	if got == nil {
		t.Fatal("expected record, got nil")
	}
	if got.GmailMsgID != "msg123" {
		t.Errorf("GmailMsgID = %q, want %q", got.GmailMsgID, "msg123")
	}
	if got.ETag != "abc" {
		t.Errorf("ETag = %q, want %q", got.ETag, "abc")
	}
	if got.Size != 1024 {
		t.Errorf("Size = %d, want 1024", got.Size)
	}
	if got.Metadata["author"] != "alex" {
		t.Errorf("Metadata[author] = %q", got.Metadata["author"])
	}
}

func TestStore_GetObject_NotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	got, err := s.GetObject(ctx, "test", "missing")
	if err != nil {
		t.Fatalf("GetObject: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil, got %+v", got)
	}
}

func TestStore_PutObject_Overwrite(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	rec1 := &backend.ObjectRecord{
		Bucket: "test", Key: "key", GmailMsgID: "old", ETag: "v1",
		Size: 100, ContentType: "text/plain", CreatedAt: time.Now(),
	}
	rec2 := &backend.ObjectRecord{
		Bucket: "test", Key: "key", GmailMsgID: "new", ETag: "v2",
		Size: 200, ContentType: "text/plain", CreatedAt: time.Now(),
	}

	_ = s.PutObject(ctx, rec1)
	_ = s.PutObject(ctx, rec2)

	got, _ := s.GetObject(ctx, "test", "key")
	if got.GmailMsgID != "new" {
		t.Errorf("GmailMsgID = %q, want %q", got.GmailMsgID, "new")
	}
	if got.ETag != "v2" {
		t.Errorf("ETag = %q, want %q", got.ETag, "v2")
	}
}

func TestStore_DeleteObject(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	rec := &backend.ObjectRecord{
		Bucket: "test", Key: "del", GmailMsgID: "m1", ETag: "e",
		Size: 10, ContentType: "text/plain", CreatedAt: time.Now(),
	}
	_ = s.PutObject(ctx, rec)
	_ = s.DeleteObject(ctx, "test", "del")

	got, _ := s.GetObject(ctx, "test", "del")
	if got != nil {
		t.Error("expected nil after delete")
	}
}

// -------------------------------------------------------------------------
// LIST OBJECTS
// -------------------------------------------------------------------------

func TestStore_ListObjects_Prefix(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	for _, key := range []string{"photos/a.jpg", "photos/b.jpg", "docs/c.pdf"} {
		_ = s.PutObject(ctx, &backend.ObjectRecord{
			Bucket: "test", Key: key, GmailMsgID: "m", ETag: "e",
			Size: 10, ContentType: "x", CreatedAt: time.Now(),
		})
	}

	got, err := s.ListObjects(ctx, "test", "photos/", "", 100)
	if err != nil {
		t.Fatalf("ListObjects: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].Key != "photos/a.jpg" {
		t.Errorf("got[0].Key = %q", got[0].Key)
	}
}

func TestStore_ListObjects_StartAfter(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	for _, key := range []string{"a", "b", "c", "d"} {
		_ = s.PutObject(ctx, &backend.ObjectRecord{
			Bucket: "test", Key: key, GmailMsgID: "m", ETag: "e",
			Size: 10, ContentType: "x", CreatedAt: time.Now(),
		})
	}

	got, err := s.ListObjects(ctx, "test", "", "b", 100)
	if err != nil {
		t.Fatalf("ListObjects: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2 (c, d)", len(got))
	}
	if got[0].Key != "c" {
		t.Errorf("got[0].Key = %q, want c", got[0].Key)
	}
}

func TestStore_ListObjects_MaxKeys(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	for _, key := range []string{"a", "b", "c"} {
		_ = s.PutObject(ctx, &backend.ObjectRecord{
			Bucket: "test", Key: key, GmailMsgID: "m", ETag: "e",
			Size: 10, ContentType: "x", CreatedAt: time.Now(),
		})
	}

	got, _ := s.ListObjects(ctx, "test", "", "", 2)
	if len(got) != 2 {
		t.Errorf("len = %d, want 2", len(got))
	}
}

// -------------------------------------------------------------------------
// BUCKET CRUD
// -------------------------------------------------------------------------

func TestStore_PutAndGetBucket(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_ = s.PutBucket(ctx, &backend.BucketRecord{
		Name: "photos", LabelID: "Label_1", CreatedAt: time.Now(),
	})

	got, err := s.GetBucket(ctx, "photos")
	if err != nil {
		t.Fatalf("GetBucket: %v", err)
	}
	if got == nil {
		t.Fatal("expected record")
	}
	if got.LabelID != "Label_1" {
		t.Errorf("LabelID = %q", got.LabelID)
	}
}

func TestStore_GetBucket_NotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	got, _ := s.GetBucket(ctx, "missing")
	if got != nil {
		t.Error("expected nil")
	}
}

func TestStore_ListBuckets(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_ = s.PutBucket(ctx, &backend.BucketRecord{Name: "b", LabelID: "L2", CreatedAt: time.Now()})
	_ = s.PutBucket(ctx, &backend.BucketRecord{Name: "a", LabelID: "L1", CreatedAt: time.Now()})

	got, _ := s.ListBuckets(ctx)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].Name != "a" {
		t.Errorf("got[0].Name = %q, want a (sorted)", got[0].Name)
	}
}

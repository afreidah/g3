// -------------------------------------------------------------------------------
// Gmail List Tests - Index-Served ListObjects
//
// Author: Alex Freidah
//
// Unit tests for the metadata-index path of ListObjects and the shared
// delimiter/truncation post-processing. These exercise the store-served listing
// without touching the Gmail API, covering the fix that replaces the N+1 Gmail
// fetch with a single indexed query.
// -------------------------------------------------------------------------------

package backend

import (
	"context"
	"errors"
	"testing"
	"time"
)

// -------------------------------------------------------------------------
// FAKE STORE
// -------------------------------------------------------------------------

// fakeListStore is a minimal MetadataStore that serves a fixed set of records
// for ListObjects and applies prefix, startAfter, and maxKeys the same way the
// real store query does. Only ListObjects is used by the listing path.
type fakeListStore struct {
	records []*ObjectRecord
	err     error
}

func (f *fakeListStore) ListObjects(_ context.Context, bucket, prefix, startAfter string, maxKeys int) ([]*ObjectRecord, error) {
	if f.err != nil {
		return nil, f.err
	}
	var out []*ObjectRecord
	for _, r := range f.records {
		if r.Bucket != bucket {
			continue
		}
		if prefix != "" && !hasPrefix(r.Key, prefix) {
			continue
		}
		if startAfter != "" && r.Key <= startAfter {
			continue
		}
		out = append(out, r)
		if len(out) >= maxKeys {
			break
		}
	}
	return out, nil
}

func (f *fakeListStore) PutObject(context.Context, *ObjectRecord) error { return nil }
func (f *fakeListStore) GetObject(context.Context, string, string) (*ObjectRecord, error) {
	return nil, nil
}
func (f *fakeListStore) DeleteObject(context.Context, string, string) error { return nil }
func (f *fakeListStore) PutBucket(context.Context, *BucketRecord) error     { return nil }
func (f *fakeListStore) GetBucket(context.Context, string) (*BucketRecord, error) {
	return nil, nil
}
func (f *fakeListStore) ListBuckets(context.Context) ([]*BucketRecord, error) { return nil, nil }

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

// -------------------------------------------------------------------------
// LIST FROM STORE
// -------------------------------------------------------------------------

func TestListFromStore_ReturnsIndexedObjects(t *testing.T) {
	now := time.UnixMilli(1700000000000)
	store := &fakeListStore{records: []*ObjectRecord{
		{Bucket: "b", Key: "a.txt", Size: 10, ETag: "etag-a", CreatedAt: now},
		{Bucket: "b", Key: "b.txt", Size: 20, ETag: "etag-b", CreatedAt: now},
		{Bucket: "b", Key: "b.txt#chunk-001", Size: 5, ETag: "etag-c", CreatedAt: now},
		{Bucket: "other", Key: "c.txt", Size: 30, ETag: "etag-d", CreatedAt: now},
	}}
	g := &GmailBackend{store: store}

	res, err := g.ListObjects(context.Background(), "b", "", "", "", 1000)
	if err != nil {
		t.Fatalf("ListObjects: %v", err)
	}
	if len(res.Contents) != 2 {
		t.Fatalf("expected 2 objects (chunk and other bucket excluded), got %d", len(res.Contents))
	}
	if res.Contents[0].Key != "a.txt" || res.Contents[0].ETag != "etag-a" || res.Contents[0].Size != 10 {
		t.Errorf("unexpected first object: %+v", res.Contents[0])
	}
}

func TestListFromStore_StartAfter(t *testing.T) {
	store := &fakeListStore{records: []*ObjectRecord{
		{Bucket: "b", Key: "a.txt"},
		{Bucket: "b", Key: "b.txt"},
		{Bucket: "b", Key: "c.txt"},
	}}
	g := &GmailBackend{store: store}

	res, err := g.ListObjects(context.Background(), "b", "", "", "a.txt", 1000)
	if err != nil {
		t.Fatalf("ListObjects: %v", err)
	}
	if len(res.Contents) != 2 || res.Contents[0].Key != "b.txt" {
		t.Errorf("startAfter not applied: %+v", res.Contents)
	}
}

func TestListFromStore_ErrorFallsBackToGmail(t *testing.T) {
	// On store error and a nil Gmail client, ListObjects must attempt the Gmail
	// fallback path rather than return the store error. The fallback then fails
	// on the nil client, which confirms the fallback was taken.
	store := &fakeListStore{err: errors.New("db down")}
	g := &GmailBackend{store: store}

	defer func() {
		if recover() == nil {
			t.Errorf("expected Gmail fallback to be attempted on store error")
		}
	}()
	_, _ = g.ListObjects(context.Background(), "b", "", "", "", 1000)
}

// -------------------------------------------------------------------------
// COLLAPSE LIST RESULT
// -------------------------------------------------------------------------

func TestCollapseListResult_Delimiter(t *testing.T) {
	objects := []ObjectInfo{
		{Key: "photos/a.jpg"},
		{Key: "photos/b.jpg"},
		{Key: "top.txt"},
	}
	res := collapseListResult(objects, "", "/", 1000)

	if len(res.CommonPrefixes) != 1 || res.CommonPrefixes[0] != "photos/" {
		t.Errorf("expected common prefix photos/, got %v", res.CommonPrefixes)
	}
	if len(res.Contents) != 1 || res.Contents[0].Key != "top.txt" {
		t.Errorf("expected only top.txt in contents, got %+v", res.Contents)
	}
}

func TestCollapseListResult_Truncation(t *testing.T) {
	objects := []ObjectInfo{
		{Key: "a"}, {Key: "b"}, {Key: "c"},
	}
	res := collapseListResult(objects, "", "", 2)

	if !res.IsTruncated {
		t.Errorf("expected IsTruncated")
	}
	if res.NextStartAfter != "b" {
		t.Errorf("expected NextStartAfter=b, got %q", res.NextStartAfter)
	}
	if len(res.Contents) != 2 {
		t.Errorf("expected 2 contents, got %d", len(res.Contents))
	}
}

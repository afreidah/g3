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
	"fmt"
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
	res := collapseListResult(objects, "", "/", 1000, false, "")

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
	res := collapseListResult(objects, "", "", 2, false, "")

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

// -------------------------------------------------------------------------
// TRUNCATION
// -------------------------------------------------------------------------

// storeWithKeys builds a fake store holding n sequentially named objects.
func storeWithKeys(bucket string, n int) *fakeListStore {
	records := make([]*ObjectRecord, 0, n)
	for i := range n {
		records = append(records, &ObjectRecord{
			Bucket: bucket,
			Key:    fmt.Sprintf("key-%04d", i),
			Size:   int64(i),
		})
	}
	return &fakeListStore{records: records}
}

// TestListFromStore_TruncatesAtMaxKeys asserts a bucket holding more than
// maxKeys objects reports the page as truncated. A full page returned as the
// whole bucket is what lets a client conclude the remaining objects were
// deleted.
func TestListFromStore_TruncatesAtMaxKeys(t *testing.T) {
	g := &GmailBackend{store: storeWithKeys("b", 25)}

	res, err := g.ListObjects(context.Background(), "b", "", "", "", 10)
	if err != nil {
		t.Fatalf("ListObjects: %v", err)
	}
	if len(res.Contents) != 10 {
		t.Fatalf("page size = %d, want 10", len(res.Contents))
	}
	if !res.IsTruncated {
		t.Error("IsTruncated = false with 15 objects still unlisted")
	}
	if res.NextStartAfter != "key-0009" {
		t.Errorf("NextStartAfter = %q, want key-0009 (last key of the page)", res.NextStartAfter)
	}
	// The sentinel object read past the page must not be served to the client.
	if res.Contents[len(res.Contents)-1].Key != "key-0009" {
		t.Errorf("last key = %q, want key-0009", res.Contents[len(res.Contents)-1].Key)
	}
}

// TestListFromStore_ExactPageIsNotTruncated covers the boundary: a bucket
// holding exactly maxKeys objects is complete, not truncated.
func TestListFromStore_ExactPageIsNotTruncated(t *testing.T) {
	g := &GmailBackend{store: storeWithKeys("b", 10)}

	res, err := g.ListObjects(context.Background(), "b", "", "", "", 10)
	if err != nil {
		t.Fatalf("ListObjects: %v", err)
	}
	if len(res.Contents) != 10 {
		t.Fatalf("page size = %d, want 10", len(res.Contents))
	}
	if res.IsTruncated {
		t.Error("IsTruncated = true on a bucket that ends exactly at the page boundary")
	}
	if res.NextStartAfter != "" {
		t.Errorf("NextStartAfter = %q, want empty", res.NextStartAfter)
	}
}

// TestListFromStore_PaginationReachesEveryKey walks the listing the way an S3
// client does, asserting that following NextStartAfter to exhaustion yields
// every object exactly once and terminates.
func TestListFromStore_PaginationReachesEveryKey(t *testing.T) {
	const total, pageSize = 25, 10
	g := &GmailBackend{store: storeWithKeys("b", total)}

	seen := make([]string, 0, total)
	cursor := ""
	for pages := 0; ; pages++ {
		if pages > total {
			t.Fatal("pagination did not terminate")
		}
		res, err := g.ListObjects(context.Background(), "b", "", "", cursor, pageSize)
		if err != nil {
			t.Fatalf("ListObjects: %v", err)
		}
		for _, obj := range res.Contents {
			seen = append(seen, obj.Key)
		}
		if !res.IsTruncated {
			break
		}
		cursor = res.NextStartAfter
	}

	if len(seen) != total {
		t.Fatalf("paginated to %d keys, want %d", len(seen), total)
	}
	for i, key := range seen {
		if want := fmt.Sprintf("key-%04d", i); key != want {
			t.Errorf("key %d = %q, want %q", i, key, want)
		}
	}
}

// TestCollapseListResult_TruncationSurvivesDelimiter asserts truncation is
// decided before the delimiter collapse. Folding the page into common prefixes
// first would absorb the sentinel object and report a truncated listing as
// complete.
func TestCollapseListResult_TruncationSurvivesDelimiter(t *testing.T) {
	objects := make([]ObjectInfo, 0, 4)
	for _, key := range []string{"dir/a", "dir/b", "dir/c", "dir/d"} {
		objects = append(objects, ObjectInfo{Key: key})
	}

	res := collapseListResult(objects, "", "/", 3, false, "")

	if !res.IsTruncated {
		t.Error("IsTruncated = false; the fourth object is past the page")
	}
	if res.NextStartAfter != "dir/c" {
		t.Errorf("NextStartAfter = %q, want dir/c", res.NextStartAfter)
	}
	if len(res.CommonPrefixes) != 1 || res.CommonPrefixes[0] != "dir/" {
		t.Errorf("CommonPrefixes = %v, want [dir/]", res.CommonPrefixes)
	}
}

// TestListFromStore_ChunkRowDoesNotHideTruncation covers a corrupt index that
// holds a chunk component: it is excluded from the listing, and the page it
// occupied still reports truncation honestly.
func TestListFromStore_ChunkRowDoesNotHideTruncation(t *testing.T) {
	store := &fakeListStore{records: []*ObjectRecord{
		{Bucket: "b", Key: "a.txt"},
		{Bucket: "b", Key: "b.txt"},
		{Bucket: "b", Key: "b.txt#chunk-001"},
		{Bucket: "b", Key: "c.txt"},
	}}
	g := &GmailBackend{store: store}

	res, err := g.ListObjects(context.Background(), "b", "", "", "", 2)
	if err != nil {
		t.Fatalf("ListObjects: %v", err)
	}
	for _, obj := range res.Contents {
		if obj.Key == "b.txt#chunk-001" {
			t.Error("chunk component surfaced as an object")
		}
	}
	if !res.IsTruncated {
		t.Error("IsTruncated = false with c.txt still unlisted")
	}
}

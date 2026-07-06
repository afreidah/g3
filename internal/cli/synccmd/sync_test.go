// -------------------------------------------------------------------------------
// g3 - Sync Subcommand Tests
//
// Author: Alex Freidah
//
// Exercises the scan/index helpers and Run flow with a stub gmailAPI and a fake
// metadata store, so the command logic is covered without a live Gmail account.
// -------------------------------------------------------------------------------

package synccmd

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/afreidah/g3/internal/backend"
	"github.com/afreidah/g3/internal/config"

	"google.golang.org/api/gmail/v1"
)

// -------------------------------------------------------------------------
// STUBS
// -------------------------------------------------------------------------

type stubGmail struct {
	labels   []*gmail.Label
	messages []*gmail.Message
	byID     map[string]*gmail.Message
	listErr  error
	getErr   error
}

func (s *stubGmail) ListLabels(context.Context) ([]*gmail.Label, error) {
	return s.labels, s.listErr
}

func (s *stubGmail) ListMessages(_ context.Context, _, _ string, _ int64) ([]*gmail.Message, string, error) {
	if s.listErr != nil {
		return nil, "", s.listErr
	}
	return s.messages, "", nil
}

func (s *stubGmail) GetMessage(_ context.Context, id, _ string) (*gmail.Message, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	return s.byID[id], nil
}

// fakeStore records PutBucket/PutObject calls and satisfies backend.MetadataStore.
type fakeStore struct {
	buckets []string
	objects []string
}

func (f *fakeStore) PutObject(_ context.Context, rec *backend.ObjectRecord) error {
	f.objects = append(f.objects, rec.Key)
	return nil
}
func (f *fakeStore) PutBucket(_ context.Context, rec *backend.BucketRecord) error {
	f.buckets = append(f.buckets, rec.Name)
	return nil
}
func (f *fakeStore) GetObject(context.Context, string, string) (*backend.ObjectRecord, error) {
	return nil, nil
}
func (f *fakeStore) DeleteObject(context.Context, string, string) error { return nil }
func (f *fakeStore) ListObjects(context.Context, string, string, string, int) ([]*backend.ObjectRecord, error) {
	return nil, nil
}
func (f *fakeStore) GetBucket(context.Context, string) (*backend.BucketRecord, error) {
	return nil, nil
}
func (f *fakeStore) ListBuckets(context.Context) ([]*backend.BucketRecord, error) { return nil, nil }

func plainMessage(id, subject, bodyJSON string) *gmail.Message {
	return &gmail.Message{
		Id: id,
		Payload: &gmail.MessagePart{
			MimeType: "text/plain",
			Headers:  []*gmail.MessagePartHeader{{Name: "Subject", Value: subject}},
			Body:     &gmail.MessagePartBody{Data: base64.URLEncoding.EncodeToString([]byte(bodyJSON))},
		},
	}
}

const validMetaJSON = `{"content_type":"text/plain","etag":"e","size":5}`

// -------------------------------------------------------------------------
// PURE HELPERS
// -------------------------------------------------------------------------

func TestBucketNameFromLabel(t *testing.T) {
	tests := []struct {
		label, prefix, want string
		ok                  bool
	}{
		{"s3/photos", "s3/", "photos", true},
		{"other/photos", "s3/", "", false},
		{"s3/", "s3/", "", false},
		{"s3/photos/sub", "s3/", "", false},
	}
	for _, tt := range tests {
		got, ok := bucketNameFromLabel(tt.label, tt.prefix)
		if got != tt.want || ok != tt.ok {
			t.Errorf("bucketNameFromLabel(%q,%q) = (%q,%v), want (%q,%v)", tt.label, tt.prefix, got, ok, tt.want, tt.ok)
		}
	}
}

func TestGmailHeaderValue(t *testing.T) {
	hdrs := []*gmail.MessagePartHeader{{Name: "Subject", Value: "x"}, {Name: "Date", Value: "y"}}
	if v := gmailHeaderValue(hdrs, "Subject"); v != "x" {
		t.Errorf("got %q", v)
	}
	if v := gmailHeaderValue(hdrs, "Missing"); v != "" {
		t.Errorf("expected empty, got %q", v)
	}
}

// Body extraction is covered by backend.ExtractBodyText's tests
// (internal/backend/gmail_test.go); synccmd now delegates to it.

// -------------------------------------------------------------------------
// DISCOVER / INDEX / SYNC
// -------------------------------------------------------------------------

func TestDiscoverBuckets(t *testing.T) {
	client := &stubGmail{labels: []*gmail.Label{
		{Name: "s3/photos", Id: "L1"},
		{Name: "s3/docs", Id: "L2"},
		{Name: "other", Id: "L3"},
		{Name: "s3/nested/x", Id: "L4"},
	}}
	store := &fakeStore{}

	buckets, err := discoverBuckets(context.Background(), client, store, "s3/")
	if err != nil {
		t.Fatalf("discoverBuckets: %v", err)
	}
	if len(buckets) != 2 {
		t.Fatalf("got %d buckets, want 2: %+v", len(buckets), buckets)
	}
	if len(store.buckets) != 2 {
		t.Errorf("store recorded %d buckets, want 2", len(store.buckets))
	}
}

func TestDiscoverBuckets_Error(t *testing.T) {
	client := &stubGmail{listErr: errors.New("boom")}
	if _, err := discoverBuckets(context.Background(), client, &fakeStore{}, "s3/"); err == nil {
		t.Fatal("expected error")
	}
}

func TestIndexMessage(t *testing.T) {
	tests := []struct {
		name    string
		msg     *gmail.Message
		want    bool
		wantObj int
	}{
		{"valid", plainMessage("m1", "s3://b/key.txt", validMetaJSON), true, 1},
		{"wrong subject", plainMessage("m1", "not-an-object", validMetaJSON), false, 0},
		{"chunk key", plainMessage("m1", "s3://b/key.txt#chunk-001", validMetaJSON), false, 0},
		{"no body", &gmail.Message{Id: "m1", Payload: &gmail.MessagePart{Headers: []*gmail.MessagePartHeader{{Name: "Subject", Value: "s3://b/key.txt"}}}}, false, 0},
		{"bad metadata", plainMessage("m1", "s3://b/key.txt", "{invalid"), false, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &stubGmail{byID: map[string]*gmail.Message{"m1": tt.msg}}
			store := &fakeStore{}
			got := indexMessage(context.Background(), client, store, "b", "m1")
			if got != tt.want {
				t.Errorf("indexMessage = %v, want %v", got, tt.want)
			}
			if len(store.objects) != tt.wantObj {
				t.Errorf("recorded %d objects, want %d", len(store.objects), tt.wantObj)
			}
		})
	}
}

func TestIndexMessage_FetchError(t *testing.T) {
	client := &stubGmail{getErr: errors.New("fetch failed")}
	if indexMessage(context.Background(), client, &fakeStore{}, "b", "m1") {
		t.Error("expected false on fetch error")
	}
}

func TestSyncBucket(t *testing.T) {
	client := &stubGmail{
		messages: []*gmail.Message{{Id: "m1"}, {Id: "m2"}, {Id: "m3"}},
		byID: map[string]*gmail.Message{
			"m1": plainMessage("m1", "s3://b/a.txt", validMetaJSON),
			"m2": plainMessage("m2", "s3://b/b.txt", validMetaJSON),
			"m3": plainMessage("m3", "not-an-object", validMetaJSON),
		},
	}
	store := &fakeStore{}
	n := syncBucket(context.Background(), client, store, "s3", bucketLabel{name: "b", labelID: "L1"})
	if n != 2 {
		t.Errorf("indexed %d, want 2", n)
	}
}

func TestSyncBucket_ListError(t *testing.T) {
	client := &stubGmail{listErr: errors.New("list failed")}
	if n := syncBucket(context.Background(), client, &fakeStore{}, "s3", bucketLabel{name: "b"}); n != 0 {
		t.Errorf("indexed %d, want 0", n)
	}
}

// -------------------------------------------------------------------------
// RUN
// -------------------------------------------------------------------------

func TestRun_Success(t *testing.T) {
	origLoad, origStore, origClient := loadConfig, openStore, newGmailClient
	t.Cleanup(func() { loadConfig, openStore, newGmailClient = origLoad, origStore, origClient })

	loadConfig = func(string) (*config.Config, error) {
		return &config.Config{Gmail: config.GmailConfig{LabelPrefix: "s3"}}, nil
	}
	openStore = func(context.Context, *config.DatabaseConfig) (backend.MetadataStore, func(), error) {
		return &fakeStore{}, func() {}, nil
	}
	newGmailClient = func(context.Context, *config.GmailConfig) (gmailAPI, error) {
		return &stubGmail{
			labels:   []*gmail.Label{{Name: "s3/b", Id: "L1"}},
			messages: []*gmail.Message{{Id: "m1"}},
			byID:     map[string]*gmail.Message{"m1": plainMessage("m1", "s3://b/a.txt", validMetaJSON)},
		}, nil
	}

	var out, errOut bytes.Buffer
	code := Run(context.Background(), nil, &out, &errOut)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr=%q)", code, errOut.String())
	}
	if !strings.Contains(out.String(), "1 bucket(s), 1 object(s)") {
		t.Errorf("summary = %q", out.String())
	}
}

func TestRun_ConfigError(t *testing.T) {
	loadConfig = func(string) (*config.Config, error) { return nil, errors.New("nope") }
	t.Cleanup(func() { loadConfig = config.LoadConfig })

	var out, errOut bytes.Buffer
	if code := Run(context.Background(), nil, &out, &errOut); code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
}

func TestRun_StoreError(t *testing.T) {
	origLoad, origStore := loadConfig, openStore
	t.Cleanup(func() { loadConfig, openStore = origLoad, origStore })

	loadConfig = func(string) (*config.Config, error) { return &config.Config{}, nil }
	openStore = func(context.Context, *config.DatabaseConfig) (backend.MetadataStore, func(), error) {
		return nil, nil, errors.New("db down")
	}

	var out, errOut bytes.Buffer
	if code := Run(context.Background(), nil, &out, &errOut); code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "metadata store") {
		t.Errorf("stderr = %q", errOut.String())
	}
}

func TestRun_GmailClientError(t *testing.T) {
	origLoad, origStore, origClient := loadConfig, openStore, newGmailClient
	t.Cleanup(func() { loadConfig, openStore, newGmailClient = origLoad, origStore, origClient })

	loadConfig = func(string) (*config.Config, error) { return &config.Config{}, nil }
	openStore = func(context.Context, *config.DatabaseConfig) (backend.MetadataStore, func(), error) {
		return &fakeStore{}, func() {}, nil
	}
	newGmailClient = func(context.Context, *config.GmailConfig) (gmailAPI, error) {
		return nil, errors.New("oauth failed")
	}

	var out, errOut bytes.Buffer
	if code := Run(context.Background(), nil, &out, &errOut); code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
}

func TestRun_BadFlag(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := Run(context.Background(), []string{"-nope"}, &out, &errOut); code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
}

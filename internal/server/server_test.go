// -------------------------------------------------------------------------------
// Server Tests - HTTP Handler Tests with Mock Backend
//
// Author: Alex Freidah
//
// Mock-based tests for S3 HTTP handlers covering PUT, GET, HEAD, DELETE,
// ListObjects, ListBuckets, CreateBucket, HeadBucket, GetBucketLocation,
// and multipart upload operations. Uses gomock to verify backend interactions.
// -------------------------------------------------------------------------------

package server

import (
	"bytes"
	"context"
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/afreidah/g3/internal/auth"
	"github.com/afreidah/g3/internal/backend"
	"github.com/afreidah/g3/internal/config"

	"go.uber.org/mock/gomock"
)

// -------------------------------------------------------------------------
// TEST HELPERS
// -------------------------------------------------------------------------

// newTestServer creates a Server with a mock backend and a registry that
// maps "testkey" to the "test" bucket.
func newTestServer(t *testing.T, ctrl *gomock.Controller) (*Server, *MockObjectBackend) {
	t.Helper()
	mock := NewMockObjectBackend(ctrl)
	registry := auth.NewBucketRegistry([]config.BucketConfig{
		{
			Name: "test",
			Credentials: []config.CredentialConfig{
				{AccessKeyID: "testkey", SecretAccessKey: "testsecret"},
			},
		},
	})
	srv := New(mock, registry)
	return srv, mock
}

// -------------------------------------------------------------------------
// PUT OBJECT
// -------------------------------------------------------------------------

func TestHandlePut_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	srv, mock := newTestServer(t, ctrl)

	mock.EXPECT().
		PutObject(gomock.Any(), "test", "hello.txt", gomock.Any(), int64(5), "text/plain", gomock.Any()).
		Return("abc123", nil)

	ctx := context.Background()
	req := httptest.NewRequest(http.MethodPut, "/test/hello.txt", strings.NewReader("hello"))
	req.Header.Set("Content-Type", "text/plain")
	req.ContentLength = 5
	rr := httptest.NewRecorder()

	status, size, err := srv.handlePut(ctx, rr, req, "test", "hello.txt")

	if status != http.StatusOK {
		t.Errorf("status = %d, want %d", status, http.StatusOK)
	}
	if size != 5 {
		t.Errorf("size = %d, want 5", size)
	}
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if rr.Header().Get("ETag") != `"abc123"` {
		t.Errorf("ETag = %q, want %q", rr.Header().Get("ETag"), `"abc123"`)
	}
}

func TestHandlePut_MissingContentLength(t *testing.T) {
	ctrl := gomock.NewController(t)
	srv, _ := newTestServer(t, ctrl)

	ctx := context.Background()
	req := httptest.NewRequest(http.MethodPut, "/test/hello.txt", strings.NewReader("hello"))
	req.ContentLength = -1
	rr := httptest.NewRecorder()

	status, _, _ := srv.handlePut(ctx, rr, req, "test", "hello.txt")

	if status != http.StatusLengthRequired {
		t.Errorf("status = %d, want %d", status, http.StatusLengthRequired)
	}
}

func TestHandlePut_BackendError(t *testing.T) {
	ctrl := gomock.NewController(t)
	srv, mock := newTestServer(t, ctrl)

	mock.EXPECT().
		PutObject(gomock.Any(), "test", "fail.txt", gomock.Any(), int64(4), gomock.Any(), gomock.Any()).
		Return("", &backend.S3Error{StatusCode: 507, Code: "InsufficientStorage", Message: "full"})

	ctx := context.Background()
	req := httptest.NewRequest(http.MethodPut, "/test/fail.txt", strings.NewReader("data"))
	req.ContentLength = 4
	rr := httptest.NewRecorder()

	status, _, err := srv.handlePut(ctx, rr, req, "test", "fail.txt")

	if status != 507 {
		t.Errorf("status = %d, want 507", status)
	}
	if err == nil {
		t.Error("expected error")
	}
}

// -------------------------------------------------------------------------
// GET OBJECT
// -------------------------------------------------------------------------

func TestHandleGet_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	srv, mock := newTestServer(t, ctrl)

	mock.EXPECT().
		GetObject(gomock.Any(), "test", "hello.txt").
		Return(&backend.GetObjectResult{
			Body:         io.NopCloser(strings.NewReader("hello")),
			Size:         5,
			ContentType:  "text/plain",
			ETag:         "abc123",
			LastModified: time.Date(2026, 3, 27, 0, 0, 0, 0, time.UTC),
			Metadata:     map[string]string{"author": "alex"},
		}, nil)

	ctx := context.Background()
	rr := httptest.NewRecorder()

	status, respSize, err := srv.handleGet(ctx, rr, "test", "hello.txt")

	if status != http.StatusOK {
		t.Errorf("status = %d, want %d", status, http.StatusOK)
	}
	if respSize != 5 {
		t.Errorf("respSize = %d, want 5", respSize)
	}
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if rr.Body.String() != "hello" {
		t.Errorf("body = %q, want %q", rr.Body.String(), "hello")
	}
	if rr.Header().Get("Content-Type") != "text/plain" {
		t.Errorf("Content-Type = %q", rr.Header().Get("Content-Type"))
	}
	if rr.Header().Get("x-amz-meta-author") != "alex" {
		t.Errorf("x-amz-meta-author = %q", rr.Header().Get("x-amz-meta-author"))
	}
}

func TestHandleGet_NotFound(t *testing.T) {
	ctrl := gomock.NewController(t)
	srv, mock := newTestServer(t, ctrl)

	mock.EXPECT().
		GetObject(gomock.Any(), "test", "missing.txt").
		Return(nil, backend.ErrObjectNotFound)

	ctx := context.Background()
	rr := httptest.NewRecorder()

	status, _, err := srv.handleGet(ctx, rr, "test", "missing.txt")

	if status != 404 {
		t.Errorf("status = %d, want 404", status)
	}
	if err == nil {
		t.Error("expected error")
	}
}

// -------------------------------------------------------------------------
// HEAD OBJECT
// -------------------------------------------------------------------------

func TestHandleHead_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	srv, mock := newTestServer(t, ctrl)

	mock.EXPECT().
		HeadObject(gomock.Any(), "test", "hello.txt").
		Return(&backend.HeadObjectResult{
			Size:         5,
			ContentType:  "text/plain",
			ETag:         "abc123",
			LastModified: time.Date(2026, 3, 27, 0, 0, 0, 0, time.UTC),
		}, nil)

	ctx := context.Background()
	rr := httptest.NewRecorder()

	status, err := srv.handleHead(ctx, rr, "test", "hello.txt")

	if status != http.StatusOK {
		t.Errorf("status = %d, want %d", status, http.StatusOK)
	}
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if rr.Header().Get("Content-Length") != "5" {
		t.Errorf("Content-Length = %q", rr.Header().Get("Content-Length"))
	}
	if rr.Body.Len() != 0 {
		t.Errorf("HEAD response should have empty body, got %d bytes", rr.Body.Len())
	}
}

// -------------------------------------------------------------------------
// DELETE OBJECT
// -------------------------------------------------------------------------

func TestHandleDelete_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	srv, mock := newTestServer(t, ctrl)

	mock.EXPECT().
		DeleteObject(gomock.Any(), "test", "hello.txt").
		Return(nil)

	ctx := context.Background()
	rr := httptest.NewRecorder()

	status, err := srv.handleDelete(ctx, rr, "test", "hello.txt")

	if status != http.StatusNoContent {
		t.Errorf("status = %d, want %d", status, http.StatusNoContent)
	}
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

// -------------------------------------------------------------------------
// LIST OBJECTS
// -------------------------------------------------------------------------

func TestHandleListObjects_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	srv, mock := newTestServer(t, ctrl)

	mock.EXPECT().
		ListObjects(gomock.Any(), "test", "photos/", "/", "", 1000).
		Return(&backend.ListObjectsResult{
			Contents: []backend.ObjectInfo{
				{Key: "photos/cat.jpg", Size: 1024, ETag: "aaa", LastModified: time.Now()},
			},
			CommonPrefixes: []string{"photos/2024/"},
		}, nil)

	ctx := context.Background()
	req := httptest.NewRequest(http.MethodGet, "/test?prefix=photos/&delimiter=/", nil)
	rr := httptest.NewRecorder()

	status, err := srv.handleListObjects(ctx, rr, req, "test")

	if status != http.StatusOK {
		t.Errorf("status = %d, want %d", status, http.StatusOK)
	}
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !strings.Contains(rr.Body.String(), "photos/cat.jpg") {
		t.Error("response missing object key")
	}
	if !strings.Contains(rr.Body.String(), "photos/2024/") {
		t.Error("response missing common prefix")
	}
}

// -------------------------------------------------------------------------
// LIST BUCKETS
// -------------------------------------------------------------------------

func TestHandleListBuckets_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	srv, mock := newTestServer(t, ctrl)

	mock.EXPECT().
		ListBuckets(gomock.Any()).
		Return([]backend.BucketInfo{
			{Name: "test", CreationDate: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
			{Name: "backups", CreationDate: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)},
		}, nil)

	ctx := context.Background()
	rr := httptest.NewRecorder()

	status := srv.handleListBuckets(ctx, rr)

	if status != http.StatusOK {
		t.Errorf("status = %d, want %d", status, http.StatusOK)
	}

	body := rr.Body.String()
	if !strings.Contains(body, "<Name>test</Name>") {
		t.Error("response missing test bucket")
	}
	if !strings.Contains(body, "<Name>backups</Name>") {
		t.Error("response missing backups bucket")
	}
}

// -------------------------------------------------------------------------
// CREATE BUCKET
// -------------------------------------------------------------------------

func TestHandleCreateBucket_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	srv, mock := newTestServer(t, ctrl)

	mock.EXPECT().
		CreateBucket(gomock.Any(), "newbucket").
		Return(nil)

	ctx := context.Background()
	rr := httptest.NewRecorder()

	status, err := srv.handleCreateBucket(ctx, rr, "newbucket")

	if status != http.StatusOK {
		t.Errorf("status = %d, want %d", status, http.StatusOK)
	}
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if rr.Header().Get("Location") != "/newbucket" {
		t.Errorf("Location = %q", rr.Header().Get("Location"))
	}
}

func TestHandleCreateBucket_AlreadyExists(t *testing.T) {
	ctrl := gomock.NewController(t)
	srv, mock := newTestServer(t, ctrl)

	mock.EXPECT().
		CreateBucket(gomock.Any(), "existing").
		Return(backend.ErrBucketExists)

	ctx := context.Background()
	rr := httptest.NewRecorder()

	status, _ := srv.handleCreateBucket(ctx, rr, "existing")

	if status != 409 {
		t.Errorf("status = %d, want 409", status)
	}
}

// -------------------------------------------------------------------------
// HEAD BUCKET
// -------------------------------------------------------------------------

func TestHandleHeadBucket_Exists(t *testing.T) {
	ctrl := gomock.NewController(t)
	srv, mock := newTestServer(t, ctrl)

	mock.EXPECT().
		ListBuckets(gomock.Any()).
		Return([]backend.BucketInfo{{Name: "test"}}, nil)

	ctx := context.Background()
	rr := httptest.NewRecorder()

	status, err := srv.handleHeadBucket(ctx, rr, "test")

	if status != http.StatusOK {
		t.Errorf("status = %d, want %d", status, http.StatusOK)
	}
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestHandleHeadBucket_NotFound(t *testing.T) {
	ctrl := gomock.NewController(t)
	srv, mock := newTestServer(t, ctrl)

	mock.EXPECT().
		ListBuckets(gomock.Any()).
		Return([]backend.BucketInfo{{Name: "other"}}, nil)

	ctx := context.Background()
	rr := httptest.NewRecorder()

	status, _ := srv.handleHeadBucket(ctx, rr, "test")

	if status != http.StatusNotFound {
		t.Errorf("status = %d, want %d", status, http.StatusNotFound)
	}
}

// -------------------------------------------------------------------------
// GET BUCKET LOCATION
// -------------------------------------------------------------------------

func TestHandleGetBucketLocation(t *testing.T) {
	ctrl := gomock.NewController(t)
	srv, _ := newTestServer(t, ctrl)

	ctx := context.Background()
	rr := httptest.NewRecorder()

	status, err := srv.handleGetBucketLocation(ctx, rr)

	if status != http.StatusOK {
		t.Errorf("status = %d, want %d", status, http.StatusOK)
	}
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !strings.Contains(rr.Body.String(), "LocationConstraint") {
		t.Error("response missing LocationConstraint")
	}
}

// -------------------------------------------------------------------------
// MULTIPART HANDLERS
// -------------------------------------------------------------------------

func TestHandleCreateMultipartUpload(t *testing.T) {
	ctrl := gomock.NewController(t)
	srv, _ := newTestServer(t, ctrl)

	ctx := context.Background()
	req := httptest.NewRequest(http.MethodPost, "/test/bigfile.tar?uploads", nil)
	req.Header.Set("Content-Type", "application/gzip")
	rr := httptest.NewRecorder()

	status, err := srv.handleCreateMultipartUpload(ctx, rr, req, "test", "bigfile.tar")

	if status != http.StatusOK {
		t.Errorf("status = %d, want %d", status, http.StatusOK)
	}
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Verify XML response contains upload ID
	var result initiateMultipartUploadResult
	if xml.Unmarshal(rr.Body.Bytes(), &result) != nil {
		// Body includes XML header, try stripping it
		body := strings.TrimPrefix(rr.Body.String(), xml.Header)
		if xmlErr2 := xml.Unmarshal([]byte(body), &result); xmlErr2 != nil {
			t.Fatalf("failed to parse response: %v", xmlErr2)
		}
	}
	if result.UploadId == "" {
		t.Error("expected non-empty upload ID")
	}
	if result.Bucket != "test" {
		t.Errorf("bucket = %q, want %q", result.Bucket, "test")
	}
}

func TestMultipartUpload_EndToEnd(t *testing.T) {
	ctrl := gomock.NewController(t)
	srv, mock := newTestServer(t, ctrl)

	// Create upload
	ctx := context.Background()
	req := httptest.NewRequest(http.MethodPost, "/test/assembled.bin?uploads", nil)
	rr := httptest.NewRecorder()
	_, _ = srv.handleCreateMultipartUpload(ctx, rr, req, "test", "assembled.bin")

	var initResult initiateMultipartUploadResult
	body := strings.TrimPrefix(rr.Body.String(), xml.Header)
	_ = xml.Unmarshal([]byte(body), &initResult)
	uploadID := initResult.UploadId

	// Upload part 1
	req1 := httptest.NewRequest(http.MethodPut, "/test/assembled.bin?uploadId="+uploadID+"&partNumber=1", bytes.NewReader([]byte("part1")))
	rr1 := httptest.NewRecorder()
	status1, size1, _ := srv.handleUploadPart(ctx, rr1, req1, "test", "assembled.bin")
	if status1 != http.StatusOK {
		t.Errorf("part1 status = %d", status1)
	}
	if size1 != 5 {
		t.Errorf("part1 size = %d, want 5", size1)
	}

	// Upload part 2
	req2 := httptest.NewRequest(http.MethodPut, "/test/assembled.bin?uploadId="+uploadID+"&partNumber=2", bytes.NewReader([]byte("part2")))
	rr2 := httptest.NewRecorder()
	_, _, _ = srv.handleUploadPart(ctx, rr2, req2, "test", "assembled.bin")

	// Complete — expect PutObject with assembled data
	mock.EXPECT().
		PutObject(gomock.Any(), "test", "assembled.bin", gomock.Any(), int64(10), "application/octet-stream", gomock.Nil()).
		Return("deadbeef", nil)

	completeReq := httptest.NewRequest(http.MethodPost, "/test/assembled.bin?uploadId="+uploadID, strings.NewReader("<CompleteMultipartUpload></CompleteMultipartUpload>"))
	completeRR := httptest.NewRecorder()
	status, err := srv.handleCompleteMultipartUpload(ctx, completeRR, completeReq, "test", "assembled.bin")

	if status != http.StatusOK {
		t.Errorf("complete status = %d, want %d", status, http.StatusOK)
	}
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestHandleAbortMultipartUpload(t *testing.T) {
	ctrl := gomock.NewController(t)
	srv, _ := newTestServer(t, ctrl)

	// Create then abort
	ctx := context.Background()
	req := httptest.NewRequest(http.MethodPost, "/test/abandoned.bin?uploads", nil)
	rr := httptest.NewRecorder()
	_, _ = srv.handleCreateMultipartUpload(ctx, rr, req, "test", "abandoned.bin")

	var initResult initiateMultipartUploadResult
	body := strings.TrimPrefix(rr.Body.String(), xml.Header)
	_ = xml.Unmarshal([]byte(body), &initResult)

	abortReq := httptest.NewRequest(http.MethodDelete, "/test/abandoned.bin?uploadId="+initResult.UploadId, nil)
	abortRR := httptest.NewRecorder()
	status, err := srv.handleAbortMultipartUpload(ctx, abortRR, abortReq)

	if status != http.StatusNoContent {
		t.Errorf("status = %d, want %d", status, http.StatusNoContent)
	}
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

// -------------------------------------------------------------------------
// WRITE S3 ERROR
// -------------------------------------------------------------------------

func TestWriteS3Error(t *testing.T) {
	rr := httptest.NewRecorder()
	writeS3Error(rr, 404, "NoSuchKey", "The specified key does not exist")

	if rr.Code != 404 {
		t.Errorf("status = %d, want 404", rr.Code)
	}
	if rr.Header().Get("Content-Type") != "application/xml" {
		t.Errorf("Content-Type = %q", rr.Header().Get("Content-Type"))
	}
	if !strings.Contains(rr.Body.String(), "<Code>NoSuchKey</Code>") {
		t.Error("response missing error code")
	}
}

func TestWriteStorageError_S3Error(t *testing.T) {
	rr := httptest.NewRecorder()
	status := writeStorageError(rr, backend.ErrObjectNotFound)

	if status != 404 {
		t.Errorf("status = %d, want 404", status)
	}
}

func TestWriteStorageError_GenericError(t *testing.T) {
	rr := httptest.NewRecorder()
	status := writeStorageError(rr, io.ErrUnexpectedEOF)

	if status != http.StatusBadGateway {
		t.Errorf("status = %d, want %d", status, http.StatusBadGateway)
	}
}

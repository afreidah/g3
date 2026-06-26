// -------------------------------------------------------------------------------
// Handler Error-Path Tests - Backend Failures and Request Routing
//
// Author: Alex Freidah
//
// Exercises the error and edge branches of the S3 handlers that delegate to the
// consumer-defined ObjectBackend: backend failures mapped to S3 responses, the
// ListObjects query-parameter parsing and truncation paths, multipart failure
// modes, and the ServeHTTP authentication entry point. Complements the
// success-path coverage in server_test.go.
// -------------------------------------------------------------------------------

package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/afreidah/g3/internal/backend"

	"go.uber.org/mock/gomock"
)

// -------------------------------------------------------------------------
// OBJECT HANDLER ERROR PATHS
// -------------------------------------------------------------------------

func TestHandleHead_NotFound(t *testing.T) {
	ctrl := gomock.NewController(t)
	srv, mock := newTestServer(t, ctrl)

	mock.EXPECT().
		HeadObject(gomock.Any(), "test", "missing.txt").
		Return(nil, backend.ErrObjectNotFound)

	rr := httptest.NewRecorder()
	status, err := srv.handleHead(context.Background(), rr, "test", "missing.txt")

	if status != http.StatusNotFound {
		t.Errorf("status = %d, want %d", status, http.StatusNotFound)
	}
	if err == nil {
		t.Error("expected error")
	}
}

func TestHandleDelete_BackendError(t *testing.T) {
	ctrl := gomock.NewController(t)
	srv, mock := newTestServer(t, ctrl)

	mock.EXPECT().
		DeleteObject(gomock.Any(), "test", "boom.txt").
		Return(&backend.S3Error{StatusCode: http.StatusBadGateway, Code: "InternalError", Message: "upstream failed"})

	rr := httptest.NewRecorder()
	status, err := srv.handleDelete(context.Background(), rr, "test", "boom.txt")

	if status != http.StatusBadGateway {
		t.Errorf("status = %d, want %d", status, http.StatusBadGateway)
	}
	if err == nil {
		t.Error("expected error")
	}
}

// -------------------------------------------------------------------------
// LIST OBJECTS PARSING AND ERROR PATHS
// -------------------------------------------------------------------------

func TestHandleListObjects_BackendError(t *testing.T) {
	ctrl := gomock.NewController(t)
	srv, mock := newTestServer(t, ctrl)

	mock.EXPECT().
		ListObjects(gomock.Any(), "test", "", "", "", 1000).
		Return(nil, &backend.S3Error{StatusCode: http.StatusServiceUnavailable, Code: "SlowDown", Message: "rate limited"})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rr := httptest.NewRecorder()
	status, err := srv.handleListObjects(context.Background(), rr, req, "test")

	if status != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", status, http.StatusServiceUnavailable)
	}
	if err == nil {
		t.Error("expected error")
	}
}

// TestHandleListObjects_MaxKeysAndContinuation verifies that max-keys is parsed
// and continuation-token is forwarded as start-after to the backend.
func TestHandleListObjects_MaxKeysAndContinuation(t *testing.T) {
	ctrl := gomock.NewController(t)
	srv, mock := newTestServer(t, ctrl)

	mock.EXPECT().
		ListObjects(gomock.Any(), "test", "", "", "tok-abc", 25).
		Return(&backend.ListObjectsResult{}, nil)

	req := httptest.NewRequest(http.MethodGet, "/test?max-keys=25&continuation-token=tok-abc", nil)
	rr := httptest.NewRecorder()
	status, err := srv.handleListObjects(context.Background(), rr, req, "test")

	if status != http.StatusOK {
		t.Errorf("status = %d, want %d", status, http.StatusOK)
	}
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestHandleListObjects_InvalidMaxKeysFallsBack verifies that a non-numeric or
// non-positive max-keys falls back to the 1000 default.
func TestHandleListObjects_InvalidMaxKeysFallsBack(t *testing.T) {
	ctrl := gomock.NewController(t)
	srv, mock := newTestServer(t, ctrl)

	mock.EXPECT().
		ListObjects(gomock.Any(), "test", "", "", "", 1000).
		Return(&backend.ListObjectsResult{}, nil)

	req := httptest.NewRequest(http.MethodGet, "/test?max-keys=notanumber", nil)
	rr := httptest.NewRecorder()
	status, _ := srv.handleListObjects(context.Background(), rr, req, "test")

	if status != http.StatusOK {
		t.Errorf("status = %d, want %d", status, http.StatusOK)
	}
}

// TestHandleListObjects_Truncated covers the truncation branch that emits a
// NextContinuationToken.
func TestHandleListObjects_Truncated(t *testing.T) {
	ctrl := gomock.NewController(t)
	srv, mock := newTestServer(t, ctrl)

	mock.EXPECT().
		ListObjects(gomock.Any(), "test", "", "", "", 1000).
		Return(&backend.ListObjectsResult{
			Contents:       []backend.ObjectInfo{{Key: "a.txt", Size: 1}},
			IsTruncated:    true,
			NextStartAfter: "a.txt",
		}, nil)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rr := httptest.NewRecorder()
	status, err := srv.handleListObjects(context.Background(), rr, req, "test")

	if status != http.StatusOK {
		t.Errorf("status = %d, want %d", status, http.StatusOK)
	}
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "<IsTruncated>true</IsTruncated>") {
		t.Error("response missing IsTruncated=true")
	}
	if !strings.Contains(body, "<NextContinuationToken>a.txt</NextContinuationToken>") {
		t.Error("response missing NextContinuationToken")
	}
}

// -------------------------------------------------------------------------
// BUCKET HANDLER ERROR PATHS
// -------------------------------------------------------------------------

func TestHandleListBuckets_BackendError(t *testing.T) {
	ctrl := gomock.NewController(t)
	srv, mock := newTestServer(t, ctrl)

	mock.EXPECT().
		ListBuckets(gomock.Any()).
		Return(nil, &backend.S3Error{StatusCode: http.StatusBadGateway, Code: "InternalError", Message: "down"})

	rr := httptest.NewRecorder()
	status := srv.handleListBuckets(context.Background(), rr)

	if status != http.StatusBadGateway {
		t.Errorf("status = %d, want %d", status, http.StatusBadGateway)
	}
}

func TestHandleHeadBucket_BackendError(t *testing.T) {
	ctrl := gomock.NewController(t)
	srv, mock := newTestServer(t, ctrl)

	mock.EXPECT().
		ListBuckets(gomock.Any()).
		Return(nil, &backend.S3Error{StatusCode: http.StatusBadGateway, Code: "InternalError", Message: "down"})

	rr := httptest.NewRecorder()
	status, err := srv.handleHeadBucket(context.Background(), rr, "test")

	if status != http.StatusBadGateway {
		t.Errorf("status = %d, want %d", status, http.StatusBadGateway)
	}
	if err == nil {
		t.Error("expected error")
	}
}

// -------------------------------------------------------------------------
// MULTIPART ERROR PATHS
// -------------------------------------------------------------------------

func TestHandleUploadPart_InvalidPartNumber(t *testing.T) {
	ctrl := gomock.NewController(t)
	srv, _ := newTestServer(t, ctrl)

	req := httptest.NewRequest(http.MethodPut, "/test/x?uploadId=abc&partNumber=0", strings.NewReader("data"))
	rr := httptest.NewRecorder()
	status, _, _ := srv.handleUploadPart(context.Background(), rr, req, "test", "x")

	if status != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", status, http.StatusBadRequest)
	}
}

func TestHandleUploadPart_UnknownUpload(t *testing.T) {
	ctrl := gomock.NewController(t)
	srv, _ := newTestServer(t, ctrl)

	req := httptest.NewRequest(http.MethodPut, "/test/x?uploadId=nope&partNumber=1", strings.NewReader("data"))
	rr := httptest.NewRecorder()
	status, _, err := srv.handleUploadPart(context.Background(), rr, req, "test", "x")

	if status != http.StatusNotFound {
		t.Errorf("status = %d, want %d", status, http.StatusNotFound)
	}
	if err == nil {
		t.Error("expected error for unknown upload")
	}
}

func TestHandleCompleteMultipartUpload_UnknownUpload(t *testing.T) {
	ctrl := gomock.NewController(t)
	srv, _ := newTestServer(t, ctrl)

	req := httptest.NewRequest(http.MethodPost, "/test/x?uploadId=nope", strings.NewReader("<CompleteMultipartUpload></CompleteMultipartUpload>"))
	rr := httptest.NewRecorder()
	status, err := srv.handleCompleteMultipartUpload(context.Background(), rr, req, "test", "x")

	if status != http.StatusNotFound {
		t.Errorf("status = %d, want %d", status, http.StatusNotFound)
	}
	if err == nil {
		t.Error("expected error for unknown upload")
	}
}

// TestHandleCompleteMultipartUpload_BackendError drives the assembly-then-PutObject
// path and asserts the backend failure is surfaced.
func TestHandleCompleteMultipartUpload_BackendError(t *testing.T) {
	ctrl := gomock.NewController(t)
	srv, mock := newTestServer(t, ctrl)

	ctx := context.Background()
	createReq := httptest.NewRequest(http.MethodPost, "/test/obj.bin?uploads", nil)
	createRR := httptest.NewRecorder()
	_, _ = srv.handleCreateMultipartUpload(ctx, createRR, createReq, "test", "obj.bin")

	uploadID := extractUploadID(t, createRR.Body.String())

	partReq := httptest.NewRequest(http.MethodPut, "/test/obj.bin?uploadId="+uploadID+"&partNumber=1", strings.NewReader("payload"))
	partRR := httptest.NewRecorder()
	_, _, _ = srv.handleUploadPart(ctx, partRR, partReq, "test", "obj.bin")

	mock.EXPECT().
		PutObject(gomock.Any(), "test", "obj.bin", gomock.Any(), int64(7), gomock.Any(), gomock.Any()).
		Return("", &backend.S3Error{StatusCode: http.StatusInsufficientStorage, Code: "InsufficientStorage", Message: "full"})

	completeReq := httptest.NewRequest(http.MethodPost, "/test/obj.bin?uploadId="+uploadID, strings.NewReader("<CompleteMultipartUpload></CompleteMultipartUpload>"))
	completeRR := httptest.NewRecorder()
	status, err := srv.handleCompleteMultipartUpload(ctx, completeRR, completeReq, "test", "obj.bin")

	if status != http.StatusInsufficientStorage {
		t.Errorf("status = %d, want %d", status, http.StatusInsufficientStorage)
	}
	if err == nil {
		t.Error("expected backend error")
	}
}

// -------------------------------------------------------------------------
// SERVEHTTP ENTRY
// -------------------------------------------------------------------------

// TestServeHTTP_AuthFailure verifies an unsigned request is rejected with 403
// and still receives a generated request ID header.
func TestServeHTTP_AuthFailure(t *testing.T) {
	ctrl := gomock.NewController(t)
	srv, _ := newTestServer(t, ctrl)

	req := httptest.NewRequest(http.MethodGet, "/test/key.txt", nil)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusForbidden)
	}
	if rr.Header().Get("X-Amz-Request-Id") == "" {
		t.Error("missing X-Amz-Request-Id header")
	}
	if !strings.Contains(rr.Body.String(), "AccessDenied") {
		t.Error("body missing AccessDenied")
	}
}

// TestServeHTTP_AdoptsValidRequestID verifies a caller-supplied valid request ID
// is echoed back rather than replaced.
func TestServeHTTP_AdoptsValidRequestID(t *testing.T) {
	ctrl := gomock.NewController(t)
	srv, _ := newTestServer(t, ctrl)

	const reqID = "abc123def456"
	req := httptest.NewRequest(http.MethodGet, "/test/key.txt", nil)
	req.Header.Set("X-Request-Id", reqID)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if got := rr.Header().Get("X-Amz-Request-Id"); got != reqID {
		t.Errorf("X-Amz-Request-Id = %q, want %q", got, reqID)
	}
}

// extractUploadID pulls the UploadId out of an InitiateMultipartUploadResult XML
// body, tolerating the XML header prefix.
func extractUploadID(t *testing.T, body string) string {
	t.Helper()
	const open, close = "<UploadId>", "</UploadId>"
	i := strings.Index(body, open)
	j := strings.Index(body, close)
	if i < 0 || j < 0 {
		t.Fatalf("no UploadId in body: %q", body)
	}
	return body[i+len(open) : j]
}

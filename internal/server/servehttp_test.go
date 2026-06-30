// -------------------------------------------------------------------------------
// ServeHTTP Routing Tests - End-to-End Dispatch with Mock Auth
//
// Author: Alex Freidah
//
// Drives full HTTP requests through Server.ServeHTTP using a mock Authenticator,
// which the consumer-defined auth interface makes possible without constructing
// valid SigV4 signatures. Covers the routing switch, authentication branches,
// request-ID propagation, and the multipart cleanup hook.
// -------------------------------------------------------------------------------

package server

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/afreidah/g3/internal/backend"

	"go.uber.org/mock/gomock"
)

// newRoutingTestServer builds a Server wired to a mock backend and a mock
// authenticator so ServeHTTP can be exercised directly.
func newRoutingTestServer(t *testing.T, ctrl *gomock.Controller) (*Server, *MockObjectBackend, *MockAuthenticator) {
	t.Helper()
	be := NewMockObjectBackend(ctrl)
	au := NewMockAuthenticator(ctrl)
	return New(be, au), be, au
}

// routeCase describes a single ServeHTTP dispatch scenario.
type routeCase struct {
	name       string
	method     string
	target     string
	body       string
	setup      func(be *MockObjectBackend)
	wantStatus int
}

// runRouteCase drives one request through ServeHTTP with auth resolving to the
// "test" bucket and asserts the status and request-ID header.
func runRouteCase(t *testing.T, tt routeCase) {
	t.Helper()
	ctrl := gomock.NewController(t)
	srv, be, au := newRoutingTestServer(t, ctrl)
	au.EXPECT().AuthenticateAndResolveBucket(gomock.Any()).Return("test", nil)
	if tt.setup != nil {
		tt.setup(be)
	}

	var body io.Reader
	if tt.body != "" {
		body = strings.NewReader(tt.body)
	}
	req := httptest.NewRequest(tt.method, tt.target, body)
	if tt.body != "" {
		req.ContentLength = int64(len(tt.body))
	}
	rr := httptest.NewRecorder()

	srv.ServeHTTP(rr, req)

	if rr.Code != tt.wantStatus {
		t.Errorf("status = %d, want %d", rr.Code, tt.wantStatus)
	}
	if rr.Header().Get("X-Amz-Request-Id") == "" {
		t.Error("missing X-Amz-Request-Id header")
	}
}

// TestServeHTTP_Routes drives every dispatch arm through ServeHTTP with auth
// resolving to the "test" bucket.
func TestServeHTTP_Routes(t *testing.T) {
	tests := []routeCase{
		{
			name: "ListBuckets root", method: http.MethodGet, target: "/",
			setup: func(be *MockObjectBackend) {
				be.EXPECT().ListBuckets(gomock.Any()).Return([]backend.BucketInfo{{Name: "test"}}, nil)
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "GetObject", method: http.MethodGet, target: "/test/k.txt",
			setup: func(be *MockObjectBackend) {
				be.EXPECT().GetObject(gomock.Any(), "test", "k.txt").Return(&backend.GetObjectResult{
					Body: io.NopCloser(strings.NewReader("hi")), Size: 2, ContentType: "text/plain",
				}, nil)
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "HeadObject", method: http.MethodHead, target: "/test/k.txt",
			setup: func(be *MockObjectBackend) {
				be.EXPECT().HeadObject(gomock.Any(), "test", "k.txt").Return(&backend.HeadObjectResult{Size: 2}, nil)
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "DeleteObject", method: http.MethodDelete, target: "/test/k.txt",
			setup: func(be *MockObjectBackend) {
				be.EXPECT().DeleteObject(gomock.Any(), "test", "k.txt").Return(nil)
			},
			wantStatus: http.StatusNoContent,
		},
		{
			name: "PutObject", method: http.MethodPut, target: "/test/k.txt", body: "hello",
			setup: func(be *MockObjectBackend) {
				be.EXPECT().PutObject(gomock.Any(), "test", "k.txt", gomock.Any(), int64(5), gomock.Any(), gomock.Any()).Return("etag", nil)
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "ListObjectsV2", method: http.MethodGet, target: "/test",
			setup: func(be *MockObjectBackend) {
				be.EXPECT().ListObjects(gomock.Any(), "test", "", "", "", 1000).Return(&backend.ListObjectsResult{}, nil)
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "HeadBucket", method: http.MethodHead, target: "/test",
			setup: func(be *MockObjectBackend) {
				be.EXPECT().ListBuckets(gomock.Any()).Return([]backend.BucketInfo{{Name: "test"}}, nil)
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "CreateBucket", method: http.MethodPut, target: "/test",
			setup: func(be *MockObjectBackend) {
				be.EXPECT().CreateBucket(gomock.Any(), "test").Return(nil)
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "GetBucketLocation", method: http.MethodGet, target: "/test?location",
			wantStatus: http.StatusOK,
		},
		{
			name: "CreateMultipartUpload", method: http.MethodPost, target: "/test/k.txt?uploads",
			wantStatus: http.StatusOK,
		},
		{
			name: "UploadPart unknown upload", method: http.MethodPut, target: "/test/k.txt?uploadId=x&partNumber=1", body: "p",
			wantStatus: http.StatusNotFound,
		},
		{
			name: "CompleteMultipartUpload unknown upload", method: http.MethodPost, target: "/test/k.txt?uploadId=x",
			wantStatus: http.StatusNotFound,
		},
		{
			name: "AbortMultipartUpload", method: http.MethodDelete, target: "/test/k.txt?uploadId=x",
			wantStatus: http.StatusNoContent,
		},
		{
			name: "method not allowed", method: http.MethodPatch, target: "/test/k.txt",
			wantStatus: http.StatusMethodNotAllowed,
		},
		{
			// GET "/" is ListBuckets; a non-GET root path fails to parse.
			name: "invalid path", method: http.MethodPut, target: "/",
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { runRouteCase(t, tt) })
	}
}

func TestServeHTTP_AuthFailure(t *testing.T) {
	ctrl := gomock.NewController(t)
	srv, _, au := newRoutingTestServer(t, ctrl)
	au.EXPECT().AuthenticateAndResolveBucket(gomock.Any()).Return("", backend.ErrObjectNotFound)

	req := httptest.NewRequest(http.MethodGet, "/test/k.txt", nil)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusForbidden)
	}
	if !strings.Contains(rr.Body.String(), "AccessDenied") {
		t.Error("body missing AccessDenied")
	}
}

func TestServeHTTP_BucketMismatch(t *testing.T) {
	ctrl := gomock.NewController(t)
	srv, _, au := newRoutingTestServer(t, ctrl)
	// Authenticated for "other" but the path targets "test".
	au.EXPECT().AuthenticateAndResolveBucket(gomock.Any()).Return("other", nil)

	req := httptest.NewRequest(http.MethodGet, "/test/k.txt", nil)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusForbidden)
	}
}

func TestServeHTTP_AdoptsValidRequestID(t *testing.T) {
	ctrl := gomock.NewController(t)
	srv, be, au := newRoutingTestServer(t, ctrl)
	au.EXPECT().AuthenticateAndResolveBucket(gomock.Any()).Return("test", nil)
	be.EXPECT().ListObjects(gomock.Any(), "test", "", "", "", 1000).Return(&backend.ListObjectsResult{}, nil)

	const reqID = "abc123def456"
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("X-Request-Id", reqID)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if got := rr.Header().Get("X-Amz-Request-Id"); got != reqID {
		t.Errorf("X-Amz-Request-Id = %q, want %q", got, reqID)
	}
}

func TestStartMultipartCleanup_StopsOnContextCancel(t *testing.T) {
	ctrl := gomock.NewController(t)
	srv, _, _ := newRoutingTestServer(t, ctrl)

	ctx, cancel := context.WithCancel(context.Background())
	srv.StartMultipartCleanup(ctx)
	cancel() // the cleanup goroutine returns on cancellation
}

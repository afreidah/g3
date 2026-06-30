// -------------------------------------------------------------------------------
// Server - S3 HTTP Request Router
//
// Author: Alex Freidah
//
// Implements the S3-compatible HTTP handler that dispatches requests to the
// appropriate operation handler based on HTTP method and path. Handles request
// ID propagation, SigV4 authentication, span creation, and audit logging for
// every request.
// -------------------------------------------------------------------------------

package server

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/afreidah/g3/internal/audit"
	"github.com/afreidah/g3/internal/telemetry"

	"go.opentelemetry.io/otel/codes"
)

// -------------------------------------------------------------------------
// SERVER
// -------------------------------------------------------------------------

// Server is the S3-compatible HTTP handler.
type Server struct {
	backend   ObjectBackend
	registry  Authenticator
	multipart *MultipartStore
}

// New creates an S3 server backed by the given ObjectBackend and
// Authenticator. Initializes an in-memory multipart upload store with
// a 1-hour TTL for abandoned uploads.
func New(b ObjectBackend, r Authenticator) *Server {
	return &Server{
		backend:   b,
		registry:  r,
		multipart: NewMultipartStore(1 * time.Hour),
	}
}

// StartMultipartCleanup begins a background goroutine that removes expired
// multipart uploads. Stops when the context is cancelled.
func (s *Server) StartMultipartCleanup(ctx context.Context) {
	s.multipart.StartCleanupLoop(ctx)
}

// -------------------------------------------------------------------------
// HTTP DISPATCH
// -------------------------------------------------------------------------

// httpSpanName maps HTTP methods to span names.
var httpSpanName = map[string]string{
	http.MethodGet:    "HTTP GET",
	http.MethodPut:    "HTTP PUT",
	http.MethodHead:   "HTTP HEAD",
	http.MethodDelete: "HTTP DELETE",
	http.MethodPost:   "HTTP POST",
}

// ServeHTTP dispatches S3 API requests. Every request gets a request ID,
// server span, and audit log entry.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	method := r.Method

	// Track inflight requests
	telemetry.InflightRequests.WithLabelValues(method).Inc()
	defer telemetry.InflightRequests.WithLabelValues(method).Dec()

	// Generate or adopt request ID
	requestID := r.Header.Get("X-Request-Id")
	if !isValidRequestID(requestID) {
		requestID = audit.NewID()
	}
	ctx := audit.WithRequestID(r.Context(), requestID)
	w.Header().Set("X-Amz-Request-Id", requestID)

	// Authenticate
	authorizedBucket, authErr := s.registry.AuthenticateAndResolveBucket(r)
	if authErr != nil {
		telemetry.AuthFailuresTotal.Inc()
		writeS3Error(w, http.StatusForbidden, "AccessDenied", "Access Denied")
		s.recordRequest(method, http.StatusForbidden, start, 0, 0)
		slog.WarnContext(ctx, "Auth failure",
			"method", method, "path", r.URL.Path, "remote", r.RemoteAddr, "error", authErr,
		)
		return
	}

	// Route: ListBuckets
	if r.URL.Path == "/" && method == http.MethodGet {
		spanName := httpSpanName[method]
		ctx, span := telemetry.StartServerSpan(ctx, spanName,
			append(telemetry.RequestAttributes(method, r.URL.Path, "", "", r.RemoteAddr),
				telemetry.AttrRequestID.String(requestID))...,
		)
		defer span.End()

		status := s.handleListBuckets(ctx, w)
		s.recordRequest(method, status, start, 0, 0)
		s.auditLog(ctx, auditRecord{
			operation: "ListBuckets",
			method:    method,
			r:         r,
			status:    status,
			start:     start,
		})
		return
	}

	// Parse path
	bucket, key, ok := parsePath(r.URL.Path)
	if !ok {
		writeS3Error(w, http.StatusBadRequest, "InvalidURI", "Could not parse the specified URI")
		s.recordRequest(method, http.StatusBadRequest, start, 0, 0)
		return
	}

	// Verify bucket matches auth
	if bucket != authorizedBucket {
		writeS3Error(w, http.StatusForbidden, "AccessDenied", "Access Denied")
		s.recordRequest(method, http.StatusForbidden, start, 0, 0)
		return
	}

	// Create span
	spanName := httpSpanName[method]
	if spanName == "" {
		spanName = "HTTP " + method
	}
	ctx, span := telemetry.StartServerSpan(ctx, spanName,
		append(telemetry.RequestAttributes(method, r.URL.Path, bucket, key, r.RemoteAddr),
			telemetry.AttrRequestID.String(requestID))...,
	)
	defer span.End()

	operation, status, reqSize, respSize, opErr := s.dispatchOperation(ctx, w, r, bucket, key)

	if opErr != nil {
		span.SetStatus(codes.Error, opErr.Error())
		span.RecordError(opErr)
	}

	s.recordRequest(method, status, start, reqSize, respSize)
	s.auditLog(ctx, auditRecord{
		operation: operation,
		method:    method,
		r:         r,
		bucket:    bucket,
		key:       key,
		status:    status,
		start:     start,
		reqSize:   reqSize,
		respSize:  respSize,
		err:       opErr,
	})
}

// dispatchOperation routes an authenticated request to the matching S3
// operation handler, returning the operation name, HTTP status, request and
// response sizes, and any handler error.
func (s *Server) dispatchOperation(ctx context.Context, w http.ResponseWriter, r *http.Request, bucket, key string) (operation string, status int, reqSize, respSize int64, err error) {
	method := r.Method

	// Check for multipart query parameters
	q := r.URL.Query()
	hasUploadID := q.Get("uploadId") != ""
	hasUploads := q.Has("uploads")

	// Route by method, key, and multipart parameters
	switch {
	case method == http.MethodPost && key != "" && hasUploads:
		operation = "CreateMultipartUpload"
		status, err = s.handleCreateMultipartUpload(ctx, w, r, bucket, key)

	case method == http.MethodPut && key != "" && hasUploadID:
		operation = "UploadPart"
		status, reqSize, err = s.handleUploadPart(ctx, w, r, bucket, key)

	case method == http.MethodPost && key != "" && hasUploadID:
		operation = "CompleteMultipartUpload"
		status, err = s.handleCompleteMultipartUpload(ctx, w, r, bucket, key)

	case method == http.MethodDelete && key != "" && hasUploadID:
		operation = "AbortMultipartUpload"
		status, err = s.handleAbortMultipartUpload(ctx, w, r)

	case method == http.MethodPut && key != "":
		operation = "PutObject"
		status, reqSize, err = s.handlePut(ctx, w, r, bucket, key)

	case method == http.MethodGet && key != "":
		operation = "GetObject"
		status, respSize, err = s.handleGet(ctx, w, bucket, key)

	case method == http.MethodHead && key != "":
		operation = "HeadObject"
		status, err = s.handleHead(ctx, w, bucket, key)

	case method == http.MethodHead && key == "":
		operation = "HeadBucket"
		status, err = s.handleHeadBucket(ctx, w, bucket)

	case method == http.MethodDelete && key != "":
		operation = "DeleteObject"
		status, err = s.handleDelete(ctx, w, bucket, key)

	case method == http.MethodGet && key == "" && q.Has("location"):
		operation = "GetBucketLocation"
		status, err = s.handleGetBucketLocation(ctx, w)

	case method == http.MethodGet && key == "":
		operation = "ListObjectsV2"
		status, err = s.handleListObjects(ctx, w, r, bucket)

	case method == http.MethodPut && key == "":
		operation = "CreateBucket"
		status, err = s.handleCreateBucket(ctx, w, bucket)

	default:
		writeS3Error(w, http.StatusMethodNotAllowed, "MethodNotAllowed", "The specified method is not allowed")
		status = http.StatusMethodNotAllowed
		operation = "Unknown"
	}

	return operation, status, reqSize, respSize, err
}

// -------------------------------------------------------------------------
// METRICS AND AUDIT
// -------------------------------------------------------------------------

// recordRequest records HTTP metrics for a completed request.
func (s *Server) recordRequest(method string, status int, start time.Time, reqSize, respSize int64) {
	statusStr := strconv.Itoa(status)
	telemetry.RequestsTotal.WithLabelValues(method, statusStr).Inc()
	telemetry.RequestDuration.WithLabelValues(method).Observe(time.Since(start).Seconds())
	if reqSize > 0 {
		telemetry.RequestSize.WithLabelValues(method).Observe(float64(reqSize))
	}
	if respSize > 0 {
		telemetry.ResponseSize.WithLabelValues(method).Observe(float64(respSize))
	}
}

// auditRecord bundles the fields describing a completed S3 operation for the
// audit log entry.
type auditRecord struct {
	operation string
	method    string
	r         *http.Request
	bucket    string
	key       string
	status    int
	start     time.Time
	reqSize   int64
	respSize  int64
	err       error
}

// auditLog emits a structured audit log entry for a completed S3 operation
// using a fixed-size backing array to avoid heap allocation.
func (s *Server) auditLog(ctx context.Context, rec auditRecord) {
	elapsed := time.Since(rec.start)
	var attrBuf [11]slog.Attr
	n := 0
	attrBuf[n] = slog.String("operation", rec.operation)
	n++
	attrBuf[n] = slog.String("method", rec.method)
	n++
	attrBuf[n] = slog.String("path", rec.r.URL.Path)
	n++
	attrBuf[n] = slog.String("remote", rec.r.RemoteAddr)
	n++
	attrBuf[n] = slog.Int("status", rec.status)
	n++
	attrBuf[n] = slog.Duration("duration", elapsed)
	n++
	if rec.bucket != "" {
		attrBuf[n] = slog.String("bucket", rec.bucket)
		n++
	}
	if rec.key != "" {
		attrBuf[n] = slog.String("key", rec.key)
		n++
	}
	if rec.reqSize > 0 {
		attrBuf[n] = slog.Int64("request_size", rec.reqSize)
		n++
	}
	if rec.respSize > 0 {
		attrBuf[n] = slog.Int64("response_size", rec.respSize)
		n++
	}
	if rec.err != nil {
		attrBuf[n] = slog.String("error", rec.err.Error())
		n++
	}
	audit.Log(ctx, "s3."+rec.operation, attrBuf[:n]...)
}

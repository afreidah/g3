// -------------------------------------------------------------------------------
// Object Handlers - PUT, GET, HEAD, DELETE
//
// Author: Alex Freidah
//
// S3 object operation handlers that delegate to the ObjectBackend. Each handler
// validates the request, calls the backend, and writes the appropriate S3
// response including headers and status codes.
// -------------------------------------------------------------------------------

package server

import (
	"context"
	"io"
	"net/http"
	"strconv"
)

// -------------------------------------------------------------------------
// PUT OBJECT
// -------------------------------------------------------------------------

// handlePut processes an S3 PutObject request. Passes the request body as an
// io.Reader to the backend for streaming upload. Returns the HTTP status,
// request body size, and any error.
func (s *Server) handlePut(ctx context.Context, w http.ResponseWriter, r *http.Request, bucket, key string) (int, int64, error) {
	contentType := r.Header.Get(headerContentType)
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	size := r.ContentLength
	if size < 0 {
		writeS3Error(w, http.StatusLengthRequired, "MissingContentLength", "Content-Length is required")
		return http.StatusLengthRequired, 0, nil
	}

	metadata := extractUserMetadata(r.Header)

	etag, err := s.backend.PutObject(ctx, bucket, key, r.Body, size, contentType, metadata)
	if err != nil {
		status := writeStorageError(w, err)
		return status, size, err
	}

	w.Header().Set("ETag", `"`+etag+`"`)
	w.WriteHeader(http.StatusOK)
	return http.StatusOK, size, nil
}

// -------------------------------------------------------------------------
// GET OBJECT
// -------------------------------------------------------------------------

// handleGet processes an S3 GetObject request. Retrieves the object from the
// backend and streams the response body. Returns the HTTP status, response
// body size, and any error.
func (s *Server) handleGet(ctx context.Context, w http.ResponseWriter, bucket, key string) (int, int64, error) {
	result, err := s.backend.GetObject(ctx, bucket, key)
	if err != nil {
		status := writeStorageError(w, err)
		return status, 0, err
	}
	defer func() { _ = result.Body.Close() }()

	w.Header().Set(headerContentType, result.ContentType)
	w.Header().Set("Content-Length", strconv.FormatInt(result.Size, 10))
	w.Header().Set("ETag", `"`+result.ETag+`"`)
	w.Header().Set("Last-Modified", result.LastModified.UTC().Format("Mon, 02 Jan 2006 15:04:05 GMT"))

	// Set user metadata headers
	for k, v := range result.Metadata {
		w.Header().Set("x-amz-meta-"+k, v)
	}

	w.Header().Set("Accept-Ranges", "none")
	w.WriteHeader(http.StatusOK)

	written, _ := io.Copy(w, result.Body)

	return http.StatusOK, written, nil
}

// -------------------------------------------------------------------------
// HEAD OBJECT
// -------------------------------------------------------------------------

// handleHead processes an S3 HeadObject request. Returns metadata without
// the object body.
func (s *Server) handleHead(ctx context.Context, w http.ResponseWriter, bucket, key string) (int, error) {
	result, err := s.backend.HeadObject(ctx, bucket, key)
	if err != nil {
		status := writeStorageError(w, err)
		return status, err
	}

	w.Header().Set(headerContentType, result.ContentType)
	w.Header().Set("Content-Length", strconv.FormatInt(result.Size, 10))
	w.Header().Set("ETag", `"`+result.ETag+`"`)
	w.Header().Set("Last-Modified", result.LastModified.UTC().Format("Mon, 02 Jan 2006 15:04:05 GMT"))

	for k, v := range result.Metadata {
		w.Header().Set("x-amz-meta-"+k, v)
	}

	w.WriteHeader(http.StatusOK)
	return http.StatusOK, nil
}

// -------------------------------------------------------------------------
// DELETE OBJECT
// -------------------------------------------------------------------------

// handleDelete processes an S3 DeleteObject request. Returns 204 No Content
// on success, matching S3 idempotent delete semantics.
func (s *Server) handleDelete(ctx context.Context, w http.ResponseWriter, bucket, key string) (int, error) {
	err := s.backend.DeleteObject(ctx, bucket, key)
	if err != nil {
		status := writeStorageError(w, err)
		return status, err
	}

	w.WriteHeader(http.StatusNoContent)
	return http.StatusNoContent, nil
}

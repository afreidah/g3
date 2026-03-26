// -------------------------------------------------------------------------------
// Object Handlers - PUT, GET, HEAD, DELETE Stubs
//
// Author: Alex Freidah
//
// Stub implementations for S3 object operations. Full implementations wired
// to the Gmail backend are added in Phase 4.
// -------------------------------------------------------------------------------

package server

import (
	"context"
	"net/http"
)

// handlePut processes an S3 PutObject request.
func (s *Server) handlePut(ctx context.Context, w http.ResponseWriter, r *http.Request, bucket, key string) (int, int64, error) {
	writeS3Error(w, http.StatusNotImplemented, "NotImplemented", "PutObject not yet implemented")
	return http.StatusNotImplemented, 0, nil
}

// handleGet processes an S3 GetObject request.
func (s *Server) handleGet(ctx context.Context, w http.ResponseWriter, bucket, key string) (int, int64, error) {
	writeS3Error(w, http.StatusNotImplemented, "NotImplemented", "GetObject not yet implemented")
	return http.StatusNotImplemented, 0, nil
}

// handleHead processes an S3 HeadObject request.
func (s *Server) handleHead(ctx context.Context, w http.ResponseWriter, bucket, key string) (int, error) {
	writeS3Error(w, http.StatusNotImplemented, "NotImplemented", "HeadObject not yet implemented")
	return http.StatusNotImplemented, nil
}

// handleDelete processes an S3 DeleteObject request.
func (s *Server) handleDelete(ctx context.Context, w http.ResponseWriter, bucket, key string) (int, error) {
	writeS3Error(w, http.StatusNotImplemented, "NotImplemented", "DeleteObject not yet implemented")
	return http.StatusNotImplemented, nil
}

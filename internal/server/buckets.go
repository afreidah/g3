// -------------------------------------------------------------------------------
// Bucket Handlers - ListBuckets and CreateBucket Stubs
//
// Author: Alex Freidah
//
// Stub implementations for S3 bucket operations. Full implementations wired
// to the Gmail backend are added in Phase 4.
// -------------------------------------------------------------------------------

package server

import (
	"context"
	"net/http"
)

// handleListBuckets processes an S3 ListBuckets request.
func (s *Server) handleListBuckets(ctx context.Context, w http.ResponseWriter) int {
	writeS3Error(w, http.StatusNotImplemented, "NotImplemented", "ListBuckets not yet implemented")
	return http.StatusNotImplemented
}

// handleCreateBucket processes an S3 CreateBucket request.
func (s *Server) handleCreateBucket(ctx context.Context, w http.ResponseWriter, bucket string) (int, error) {
	writeS3Error(w, http.StatusNotImplemented, "NotImplemented", "CreateBucket not yet implemented")
	return http.StatusNotImplemented, nil
}

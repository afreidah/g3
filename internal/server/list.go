// -------------------------------------------------------------------------------
// List Handler - ListObjectsV2 Stub
//
// Author: Alex Freidah
//
// Stub implementation for S3 ListObjectsV2. Full implementation wired to
// the Gmail backend is added in Phase 4.
// -------------------------------------------------------------------------------

package server

import (
	"context"
	"net/http"
)

// handleListObjects processes an S3 ListObjectsV2 request.
func (s *Server) handleListObjects(ctx context.Context, w http.ResponseWriter, r *http.Request, bucket string) (int, error) {
	writeS3Error(w, http.StatusNotImplemented, "NotImplemented", "ListObjectsV2 not yet implemented")
	return http.StatusNotImplemented, nil
}

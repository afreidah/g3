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
	"net/http"

	"github.com/afreidah/g3/internal/auth"
	"github.com/afreidah/g3/internal/backend"
)

// -------------------------------------------------------------------------
// SERVER
// -------------------------------------------------------------------------

// Server is the S3-compatible HTTP handler.
type Server struct {
	backend  backend.ObjectBackend
	registry *auth.BucketRegistry
}

// New creates an S3 server backed by the given ObjectBackend and
// BucketRegistry.
func New(b backend.ObjectBackend, r *auth.BucketRegistry) *Server {
	return &Server{backend: b, registry: r}
}

// ServeHTTP dispatches S3 API requests.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// TODO: implement routing, auth, spans, audit
	w.WriteHeader(http.StatusNotImplemented)
}

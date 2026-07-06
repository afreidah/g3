// -------------------------------------------------------------------------------
// Server Auth Contract - Consumer-Defined Authenticator Interface
//
// Author: Alex Freidah
//
// Declares the authentication operation the HTTP server depends on. Defining the
// interface in the consuming package keeps the dependency contract owned by the
// server and lets ServeHTTP be tested against a generated mock without building
// valid SigV4 signatures.
// -------------------------------------------------------------------------------

package server

import (
	"net/http"

	"github.com/afreidah/g3/internal/auth"
)

//go:generate mockgen -destination=mock_authenticator.go -package=server github.com/afreidah/g3/internal/server Authenticator

// Authenticator validates a request's SigV4 signature and resolves the bucket
// the presented credentials are authorized to access. It is satisfied by
// *auth.BucketRegistry.
type Authenticator interface {
	AuthenticateAndResolveBucket(r *http.Request) (string, error)
}

// Compile-time assertion that the concrete bucket registry satisfies the
// server's consumer-defined contract.
var _ Authenticator = (*auth.BucketRegistry)(nil)

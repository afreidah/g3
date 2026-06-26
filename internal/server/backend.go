// -------------------------------------------------------------------------------
// Server Backend Contract - Consumer-Defined Storage Interface
//
// Author: Alex Freidah
//
// Declares the storage operations the HTTP server depends on. Defining the
// interface in the consuming package, rather than the package that implements
// it, keeps the dependency contract owned by the server and lets handlers be
// tested against a generated mock without reaching into the concrete backend.
// -------------------------------------------------------------------------------

package server

import (
	"context"
	"io"

	"github.com/afreidah/g3/internal/backend"
)

//go:generate mockgen -destination=mock_backend.go -package=server github.com/afreidah/g3/internal/server ObjectBackend

// ObjectBackend defines the storage operations the S3 server invokes on the
// backing store. It is satisfied by *backend.GmailBackend and any future
// storage implementation.
type ObjectBackend interface {
	PutObject(ctx context.Context, bucket, key string, body io.Reader, size int64, contentType string, metadata map[string]string) (etag string, err error)
	GetObject(ctx context.Context, bucket, key string) (*backend.GetObjectResult, error)
	HeadObject(ctx context.Context, bucket, key string) (*backend.HeadObjectResult, error)
	DeleteObject(ctx context.Context, bucket, key string) error
	ListObjects(ctx context.Context, bucket, prefix, delimiter, startAfter string, maxKeys int) (*backend.ListObjectsResult, error)
	ListBuckets(ctx context.Context) ([]backend.BucketInfo, error)
	CreateBucket(ctx context.Context, bucket string) error
}

// Compile-time assertion that the concrete Gmail backend satisfies the
// server's consumer-defined contract.
var _ ObjectBackend = (*backend.GmailBackend)(nil)

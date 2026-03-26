// -------------------------------------------------------------------------------
// Backend Types - Interface and Result Definitions
//
// Author: Alex Freidah
//
// Defines the ObjectBackend interface that abstracts storage operations and
// the result types returned by read operations. Any storage backend (Gmail,
// S3, filesystem) can implement this interface to serve as the backing store.
// -------------------------------------------------------------------------------

package backend

import (
	"context"
	"io"
	"time"
)

// -------------------------------------------------------------------------
// INTERFACE
// -------------------------------------------------------------------------

//go:generate mockgen -destination=mock_generated_test.go -package=backend github.com/afreidah/g3/internal/backend ObjectBackend

// ObjectBackend defines the storage operations for the S3 gateway.
type ObjectBackend interface {
	PutObject(ctx context.Context, bucket, key string, body io.Reader, size int64, contentType string, metadata map[string]string) (etag string, err error)
	GetObject(ctx context.Context, bucket, key string) (*GetObjectResult, error)
	HeadObject(ctx context.Context, bucket, key string) (*HeadObjectResult, error)
	DeleteObject(ctx context.Context, bucket, key string) error
	ListObjects(ctx context.Context, bucket, prefix, delimiter, startAfter string, maxKeys int) (*ListObjectsResult, error)
	ListBuckets(ctx context.Context) ([]BucketInfo, error)
	CreateBucket(ctx context.Context, bucket string) error
}

// -------------------------------------------------------------------------
// RESULT TYPES
// -------------------------------------------------------------------------

// GetObjectResult holds the response data from a GetObject operation.
type GetObjectResult struct {
	Body         io.ReadCloser
	Size         int64
	ContentType  string
	ETag         string
	LastModified time.Time
	Metadata     map[string]string
}

// HeadObjectResult holds metadata from a HeadObject operation.
type HeadObjectResult struct {
	Size         int64
	ContentType  string
	ETag         string
	LastModified time.Time
	Metadata     map[string]string
}

// ListObjectsResult holds the response from a ListObjectsV2 operation.
type ListObjectsResult struct {
	Contents       []ObjectInfo
	CommonPrefixes []string
	IsTruncated    bool
	NextStartAfter string
}

// ObjectInfo describes a single object in a listing response.
type ObjectInfo struct {
	Key          string
	Size         int64
	LastModified time.Time
	ETag         string
}

// BucketInfo describes a bucket in a ListBuckets response.
type BucketInfo struct {
	Name         string
	CreationDate time.Time
}

// -------------------------------------------------------------------------
// S3 ERRORS
// -------------------------------------------------------------------------

// S3Error represents a typed error that maps to an S3 HTTP error response.
type S3Error struct {
	StatusCode int
	Code       string
	Message    string
}

// Error implements the error interface.
func (e *S3Error) Error() string {
	return e.Code + ": " + e.Message
}

// Sentinel errors for common S3 error conditions.
var (
	ErrObjectNotFound = &S3Error{StatusCode: 404, Code: "NoSuchKey", Message: "The specified key does not exist"}
	ErrBucketNotFound = &S3Error{StatusCode: 404, Code: "NoSuchBucket", Message: "The specified bucket does not exist"}
	ErrBucketExists   = &S3Error{StatusCode: 409, Code: "BucketAlreadyOwnedByYou", Message: "Your previous request to create the named bucket succeeded"}
)

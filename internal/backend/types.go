// -------------------------------------------------------------------------------
// Backend Types - Result and Error Definitions
//
// Author: Alex Freidah
//
// Defines the result types returned by backend read operations and the typed
// S3 error model. The storage contract itself is a consumer-defined interface
// owned by the packages that depend on a backend (see server.ObjectBackend).
// -------------------------------------------------------------------------------

package backend

import (
	"io"
	"time"
)

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

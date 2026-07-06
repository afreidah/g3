// -------------------------------------------------------------------------------
// Backend Types Tests
//
// Author: Alex Freidah
//
// Covers the S3Error string formatting and confirms the sentinel errors carry
// the expected HTTP status codes and S3 error codes.
// -------------------------------------------------------------------------------

package backend

import (
	"errors"
	"testing"
)

func TestS3Error_Error(t *testing.T) {
	err := &S3Error{StatusCode: 404, Code: "NoSuchKey", Message: "The specified key does not exist"}
	want := "NoSuchKey: The specified key does not exist"
	if got := err.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestS3Error_Sentinels(t *testing.T) {
	tests := []struct {
		name   string
		err    *S3Error
		status int
		code   string
	}{
		{"not found", ErrObjectNotFound, 404, "NoSuchKey"},
		{"bucket not found", ErrBucketNotFound, 404, "NoSuchBucket"},
		{"bucket exists", ErrBucketExists, 409, "BucketAlreadyOwnedByYou"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err.StatusCode != tt.status {
				t.Errorf("StatusCode = %d, want %d", tt.err.StatusCode, tt.status)
			}
			if tt.err.Code != tt.code {
				t.Errorf("Code = %q, want %q", tt.err.Code, tt.code)
			}
			// Must satisfy the error interface and be matchable with errors.Is.
			if !errors.Is(tt.err, tt.err) {
				t.Error("errors.Is against itself = false")
			}
		})
	}
}

// -------------------------------------------------------------------------------
// Auth - SigV4 Authentication and Bucket Registry
//
// Author: Alex Freidah
//
// Provides S3-compatible SigV4 request authentication and a bucket registry
// that maps access key IDs to bucket names. Credentials are loaded from the
// YAML configuration at startup.
// -------------------------------------------------------------------------------

package auth

import (
	"net/http"

	"github.com/afreidah/g3/internal/config"
)

// -------------------------------------------------------------------------
// BUCKET REGISTRY
// -------------------------------------------------------------------------

type bucketEntry struct {
	BucketName     string
	SecretAccessKey string
}

// BucketRegistry maps access key IDs to bucket names and secrets for SigV4
// verification.
type BucketRegistry struct {
	byAccessKey map[string]bucketEntry
}

// NewBucketRegistry builds a registry from the configured bucket list.
func NewBucketRegistry(buckets []config.BucketConfig) *BucketRegistry {
	r := &BucketRegistry{
		byAccessKey: make(map[string]bucketEntry),
	}
	for _, b := range buckets {
		for _, cred := range b.Credentials {
			r.byAccessKey[cred.AccessKeyID] = bucketEntry{
				BucketName:     b.Name,
				SecretAccessKey: cred.SecretAccessKey,
			}
		}
	}
	return r
}

// -------------------------------------------------------------------------
// AUTHENTICATION
// -------------------------------------------------------------------------

// AuthenticateAndResolveBucket validates the request credentials and returns
// the bucket name the caller is authorized to access.
func (r *BucketRegistry) AuthenticateAndResolveBucket(req *http.Request) (string, error) {
	// TODO: implement SigV4 verification
	// For now, extract access key from Authorization header and look up bucket
	return "", nil
}

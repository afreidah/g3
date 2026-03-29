// -------------------------------------------------------------------------------
// Auth - SigV4 Authentication and Bucket Registry
//
// Author: Alex Freidah
//
// Provides S3-compatible SigV4 request authentication and a bucket registry
// that maps access key IDs to bucket names. Credentials are loaded from the
// YAML configuration at startup. Authentication uses constant-time comparison
// and always computes a signature even for unknown keys to prevent timing
// side-channel attacks.
// -------------------------------------------------------------------------------

package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/afreidah/g3/internal/config"
)

// -------------------------------------------------------------------------
// ERRORS
// -------------------------------------------------------------------------

var (
	ErrAccessDenied     = errors.New("access denied")
	ErrMissingAuth      = errors.New("missing authorization header")
	ErrMalformedAuth    = errors.New("malformed authorization header")
	ErrExpiredSignature = errors.New("signature expired")
)

const maxClockSkew = 15 * time.Minute

// dummySecret is used when the access key is unknown so we still compute a
// signature in constant time.
const dummySecret = "g3-dummy-secret-for-constant-time-auth"

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

// AuthenticateAndResolveBucket validates the SigV4 request signature and
// returns the bucket name the caller is authorized to access.
func (r *BucketRegistry) AuthenticateAndResolveBucket(req *http.Request) (string, error) {
	authHeader := req.Header.Get("Authorization")
	if authHeader == "" {
		return "", ErrMissingAuth
	}

	if !strings.HasPrefix(authHeader, "AWS4-HMAC-SHA256 ") {
		return "", ErrMalformedAuth
	}

	fields := parseSigV4Fields(strings.TrimPrefix(authHeader, "AWS4-HMAC-SHA256 "))
	credential := fields["Credential"]
	signedHeaders := fields["SignedHeaders"]
	signature := fields["Signature"]

	if credential == "" || signedHeaders == "" || signature == "" {
		return "", ErrMalformedAuth
	}

	// Parse credential: accessKey/date/region/service/aws4_request
	credParts := strings.SplitN(credential, "/", 2)
	if len(credParts) != 2 {
		return "", ErrMalformedAuth
	}
	accessKey := credParts[0]
	credentialScope := credParts[1]

	// Look up secret (use dummy for unknown keys)
	entry, known := r.byAccessKey[accessKey]
	secret := dummySecret
	if known {
		secret = entry.SecretAccessKey
	}

	// Verify timestamp
	amzDate := req.Header.Get("X-Amz-Date")
	if amzDate != "" {
		t, err := time.Parse("20060102T150405Z", amzDate)
		if err == nil && time.Since(t).Abs() > maxClockSkew {
			return "", ErrExpiredSignature
		}
	}

	// Build canonical request and compute expected signature
	canonicalRequest := buildCanonicalRequest(req, strings.Split(signedHeaders, ";"))
	stringToSign := "AWS4-HMAC-SHA256\n" +
		amzDate + "\n" +
		credentialScope + "\n" +
		sha256Hex([]byte(canonicalRequest))

	signingKey := deriveSigningKey(secret, credentialScope)
	expectedSig := hex.EncodeToString(hmacSHA256(signingKey, []byte(stringToSign)))

	if !hmac.Equal([]byte(expectedSig), []byte(signature)) || !known {
		return "", ErrAccessDenied
	}

	return entry.BucketName, nil
}

// -------------------------------------------------------------------------
// SIGV4 INTERNALS
// -------------------------------------------------------------------------

// parseSigV4Fields extracts key-value pairs from the AWS Authorization header
// content (after the "AWS4-HMAC-SHA256 " prefix).
func parseSigV4Fields(s string) map[string]string {
	fields := make(map[string]string)
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		idx := strings.IndexByte(part, '=')
		if idx < 0 {
			continue
		}
		fields[part[:idx]] = part[idx+1:]
	}
	return fields
}

// buildCanonicalRequest constructs the canonical request string per the
// SigV4 specification.
func buildCanonicalRequest(r *http.Request, signedHeaders []string) string {
	sort.Strings(signedHeaders)

	var canonicalHeaders strings.Builder
	for _, h := range signedHeaders {
		val := ""
		if h == "host" {
			val = r.Host
		} else {
			val = r.Header.Get(h)
		}
		canonicalHeaders.WriteString(strings.ToLower(h))
		canonicalHeaders.WriteByte(':')
		canonicalHeaders.WriteString(strings.TrimSpace(val))
		canonicalHeaders.WriteByte('\n')
	}

	payloadHash := r.Header.Get("X-Amz-Content-Sha256")
	if payloadHash == "" {
		payloadHash = "UNSIGNED-PAYLOAD"
	}

	var b strings.Builder
	b.WriteString(r.Method)
	b.WriteByte('\n')
	b.WriteString(sigV4EncodePath(r.URL.Path))
	b.WriteByte('\n')
	b.WriteString(buildCanonicalQueryString(r))
	b.WriteByte('\n')
	b.WriteString(canonicalHeaders.String())
	b.WriteByte('\n')
	b.WriteString(strings.Join(signedHeaders, ";"))
	b.WriteByte('\n')
	b.WriteString(payloadHash)

	return b.String()
}

// buildCanonicalQueryString sorts and encodes query parameters per SigV4.
func buildCanonicalQueryString(r *http.Request) string {
	params := r.URL.Query()
	if len(params) == 0 {
		return ""
	}

	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// Build sorted key=value pairs, expanding multi-value params
	type kv struct{ k, v string }
	var pairs []kv
	for _, k := range keys {
		values := params[k]
		sort.Strings(values)
		for _, v := range values {
			pairs = append(pairs, kv{sigV4Encode(k), sigV4Encode(v)})
		}
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].k != pairs[j].k {
			return pairs[i].k < pairs[j].k
		}
		return pairs[i].v < pairs[j].v
	})

	var b strings.Builder
	for i, p := range pairs {
		if i > 0 {
			b.WriteByte('&')
		}
		b.WriteString(p.k)
		b.WriteByte('=')
		b.WriteString(p.v)
	}
	return b.String()
}

// sigV4EncodePath encodes the URL path per RFC 3986, preserving slashes.
func sigV4EncodePath(path string) string {
	segments := strings.Split(path, "/")
	for i, s := range segments {
		segments[i] = sigV4Encode(s)
	}
	return strings.Join(segments, "/")
}

// sigV4Encode encodes a string per RFC 3986 (spaces as %20, not +).
func sigV4Encode(s string) string {
	var b strings.Builder
	for _, c := range []byte(s) {
		if isUnreserved(c) {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

// isUnreserved returns true for RFC 3986 unreserved characters.
func isUnreserved(c byte) bool {
	return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') ||
		(c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.' || c == '~'
}

// -------------------------------------------------------------------------
// CRYPTO HELPERS
// -------------------------------------------------------------------------

// signingKeyCache caches derived signing keys per credential scope to avoid
// re-deriving on every request.
var signingKeyCache sync.Map

// deriveSigningKey computes the SigV4 signing key from the secret and
// credential scope (date/region/service/aws4_request).
func deriveSigningKey(secret, credentialScope string) []byte {
	if cached, ok := signingKeyCache.Load(secret + "/" + credentialScope); ok {
		return cached.([]byte)
	}

	parts := strings.Split(credentialScope, "/")
	if len(parts) < 3 {
		return hmacSHA256([]byte("AWS4"+secret), []byte(credentialScope))
	}

	dateKey := hmacSHA256([]byte("AWS4"+secret), []byte(parts[0]))
	regionKey := hmacSHA256(dateKey, []byte(parts[1]))
	serviceKey := hmacSHA256(regionKey, []byte(parts[2]))
	signingKey := hmacSHA256(serviceKey, []byte("aws4_request"))

	signingKeyCache.Store(secret+"/"+credentialScope, signingKey)
	return signingKey
}

// hmacSHA256 computes HMAC-SHA256.
func hmacSHA256(key, data []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return h.Sum(nil)
}

// sha256Hex computes SHA-256 and returns the hex-encoded digest.
func sha256Hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

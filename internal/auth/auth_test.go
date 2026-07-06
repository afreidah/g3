// -------------------------------------------------------------------------------
// Auth Tests
//
// Author: Alex Freidah
//
// Unit tests for SigV4 internals, bucket registry, and query string
// canonicalization.
// -------------------------------------------------------------------------------

package auth

import (
	"bytes"
	"encoding/hex"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/afreidah/g3/internal/config"
)

// -------------------------------------------------------------------------
// BUCKET REGISTRY
// -------------------------------------------------------------------------

func TestNewBucketRegistry(t *testing.T) {
	buckets := []config.BucketConfig{
		{
			Name: "photos",
			Credentials: []config.CredentialConfig{
				{AccessKeyID: "key1", SecretAccessKey: "secret1"},
				{AccessKeyID: "key2", SecretAccessKey: "secret2"},
			},
		},
		{
			Name: "backups",
			Credentials: []config.CredentialConfig{
				{AccessKeyID: "key3", SecretAccessKey: "secret3"},
			},
		},
	}

	r := NewBucketRegistry(buckets)

	tests := []struct {
		accessKey  string
		wantBucket string
		wantFound  bool
	}{
		{"key1", "photos", true},
		{"key2", "photos", true},
		{"key3", "backups", true},
		{"unknown", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.accessKey, func(t *testing.T) {
			entry, ok := r.byAccessKey[tt.accessKey]
			if ok != tt.wantFound {
				t.Fatalf("found = %v, want %v", ok, tt.wantFound)
			}
			if ok && entry.BucketName != tt.wantBucket {
				t.Errorf("bucket = %q, want %q", entry.BucketName, tt.wantBucket)
			}
		})
	}
}

// -------------------------------------------------------------------------
// SIGV4 FIELD PARSING
// -------------------------------------------------------------------------

func TestParseSigV4Fields(t *testing.T) {
	input := "Credential=AKID/20260215/us-east-1/s3/aws4_request, SignedHeaders=host;x-amz-date, Signature=abcdef1234567890"
	fields := parseSigV4Fields(input)

	if fields["Credential"] != "AKID/20260215/us-east-1/s3/aws4_request" {
		t.Errorf("Credential = %q", fields["Credential"])
	}
	if fields["SignedHeaders"] != "host;x-amz-date" {
		t.Errorf("SignedHeaders = %q", fields["SignedHeaders"])
	}
	if fields["Signature"] != "abcdef1234567890" {
		t.Errorf("Signature = %q", fields["Signature"])
	}
}

func TestParseSigV4Fields_Empty(t *testing.T) {
	fields := parseSigV4Fields("")
	if len(fields) != 0 {
		t.Errorf("expected empty map, got %v", fields)
	}
}

// -------------------------------------------------------------------------
// CANONICAL QUERY STRING
// -------------------------------------------------------------------------

func TestBuildCanonicalQueryString_MultiValue(t *testing.T) {
	u, _ := url.Parse("http://example.com/?key=b&key=a&other=val")
	r := &fakeRequest{url: u}
	got := buildCanonicalQueryStringFromURL(u)

	// key=a&key=b&other=val (sorted by key, then by value)
	if got != "key=a&key=b&other=val" {
		t.Errorf("got %q, want %q", got, "key=a&key=b&other=val")
	}
	_ = r
}

func TestBuildCanonicalQueryString_Empty(t *testing.T) {
	u, _ := url.Parse("http://example.com/")
	got := buildCanonicalQueryStringFromURL(u)
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestBuildCanonicalQueryString_SpecialChars(t *testing.T) {
	u, _ := url.Parse("http://example.com/?prefix=photos%2F2024&delimiter=%2F")
	got := buildCanonicalQueryStringFromURL(u)

	if got != "delimiter=%2F&prefix=photos%2F2024" {
		t.Errorf("got %q, want %q", got, "delimiter=%2F&prefix=photos%2F2024")
	}
}

// -------------------------------------------------------------------------
// SIGV4 ENCODING
// -------------------------------------------------------------------------

func TestSigV4Encode(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"hello", "hello"},
		{"hello world", "hello%20world"},
		{"path/to/key", "path%2Fto%2Fkey"},
		{"a+b", "a%2Bb"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := sigV4Encode(tt.input)
			if got != tt.want {
				t.Errorf("sigV4Encode(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestSigV4EncodePath(t *testing.T) {
	got := sigV4EncodePath("/bucket/path/to/key")
	if got != "/bucket/path/to/key" {
		t.Errorf("got %q, want slashes preserved", got)
	}

	got = sigV4EncodePath("/bucket/file name.txt")
	if got != "/bucket/file%20name.txt" {
		t.Errorf("got %q, want spaces encoded", got)
	}
}

// -------------------------------------------------------------------------
// CRYPTO HELPERS
// -------------------------------------------------------------------------

func TestSha256Hex(t *testing.T) {
	got := sha256Hex([]byte(""))
	want := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	if got != want {
		t.Errorf("sha256Hex empty = %q, want %q", got, want)
	}
}

func TestDeriveSigningKey_Cached(t *testing.T) {
	key1 := deriveSigningKey("secret", "20260101/us-east-1/s3/aws4_request")
	key2 := deriveSigningKey("secret", "20260101/us-east-1/s3/aws4_request")

	if len(key1) == 0 {
		t.Fatal("expected non-empty signing key")
	}
	if !bytes.Equal(key1, key2) {
		t.Error("expected cached key to match")
	}
}

// -------------------------------------------------------------------------
// AUTHENTICATE AND RESOLVE BUCKET
// -------------------------------------------------------------------------

func TestAuthenticateAndResolveBucket_MissingHeader(t *testing.T) {
	r := NewBucketRegistry(nil)
	req, _ := http.NewRequest(http.MethodGet, "/test/key", nil)

	_, err := r.AuthenticateAndResolveBucket(req)
	if err != ErrMissingAuth {
		t.Errorf("err = %v, want ErrMissingAuth", err)
	}
}

func TestAuthenticateAndResolveBucket_WrongScheme(t *testing.T) {
	r := NewBucketRegistry(nil)
	req, _ := http.NewRequest(http.MethodGet, "/test/key", nil)
	req.Header.Set("Authorization", "Basic dXNlcjpwYXNz")

	_, err := r.AuthenticateAndResolveBucket(req)
	if err != ErrMalformedAuth {
		t.Errorf("err = %v, want ErrMalformedAuth", err)
	}
}

func TestAuthenticateAndResolveBucket_MalformedFields(t *testing.T) {
	r := NewBucketRegistry(nil)
	req, _ := http.NewRequest(http.MethodGet, "/test/key", nil)
	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 garbage")

	_, err := r.AuthenticateAndResolveBucket(req)
	if err != ErrMalformedAuth {
		t.Errorf("err = %v, want ErrMalformedAuth", err)
	}
}

func TestAuthenticateAndResolveBucket_UnknownKey(t *testing.T) {
	r := NewBucketRegistry([]config.BucketConfig{
		{Name: "test", Credentials: []config.CredentialConfig{
			{AccessKeyID: "realkey", SecretAccessKey: "realsecret"},
		}},
	})
	req, _ := http.NewRequest(http.MethodGet, "/test/key", nil)
	req.Host = "localhost:9000"
	req.Header.Set("X-Amz-Date", time.Now().UTC().Format("20060102T150405Z"))
	req.Header.Set("X-Amz-Content-Sha256", "UNSIGNED-PAYLOAD")
	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential=fakekey/20260401/us-east-1/s3/aws4_request, SignedHeaders=host;x-amz-date, Signature=0000000000000000000000000000000000000000000000000000000000000000")

	_, err := r.AuthenticateAndResolveBucket(req)
	if err != ErrAccessDenied {
		t.Errorf("err = %v, want ErrAccessDenied", err)
	}
}

func TestAuthenticateAndResolveBucket_ExpiredSignature(t *testing.T) {
	r := NewBucketRegistry([]config.BucketConfig{
		{Name: "test", Credentials: []config.CredentialConfig{
			{AccessKeyID: "key1", SecretAccessKey: "secret1"},
		}},
	})
	req, _ := http.NewRequest(http.MethodGet, "/test/key", nil)
	req.Host = "localhost:9000"
	// Set timestamp 30 minutes in the past
	req.Header.Set("X-Amz-Date", time.Now().UTC().Add(-30*time.Minute).Format("20060102T150405Z"))
	req.Header.Set("X-Amz-Content-Sha256", "UNSIGNED-PAYLOAD")
	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential=key1/20260401/us-east-1/s3/aws4_request, SignedHeaders=host;x-amz-date, Signature=0000000000000000000000000000000000000000000000000000000000000000")

	_, err := r.AuthenticateAndResolveBucket(req)
	if err != ErrExpiredSignature {
		t.Errorf("err = %v, want ErrExpiredSignature", err)
	}
}

// -------------------------------------------------------------------------
// HELPERS
// -------------------------------------------------------------------------

type fakeRequest struct {
	url *url.URL
}

// buildCanonicalQueryStringFromURL is a test helper that builds the canonical
// query string from a URL without needing a full http.Request.
func buildCanonicalQueryStringFromURL(u *url.URL) string {
	params := u.Query()
	if len(params) == 0 {
		return ""
	}

	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}

	type kv struct{ k, v string }
	var pairs []kv
	for _, k := range keys {
		for _, v := range params[k] {
			pairs = append(pairs, kv{sigV4Encode(k), sigV4Encode(v)})
		}
	}

	// Sort by key, then by value
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].k != pairs[j].k {
			return pairs[i].k < pairs[j].k
		}
		return pairs[i].v < pairs[j].v
	})

	var result string
	for i, p := range pairs {
		if i > 0 {
			result += "&"
		}
		result += p.k + "=" + p.v
	}
	return result
}

// -------------------------------------------------------------------------
// CANONICAL QUERY STRING (real implementation)
// -------------------------------------------------------------------------

// TestBuildCanonicalQueryString_Request exercises the production
// buildCanonicalQueryString against real *http.Request values, covering the
// sort/encode/multi-value paths the URL helper only approximated.
func TestBuildCanonicalQueryString_Request(t *testing.T) {
	tests := []struct {
		name    string
		rawpath string
		want    string
	}{
		{"empty", "/bucket", ""},
		{"single", "/bucket?prefix=photos", "prefix=photos"},
		{"sorted keys", "/bucket?b=2&a=1", "a=1&b=2"},
		{"multi value sorted", "/bucket?x=2&x=1", "x=1&x=2"},
		{"encoded", "/bucket?key=a b&list-type=2", "key=a%20b&list-type=2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, tt.rawpath, nil)
			if err != nil {
				t.Fatalf("NewRequest: %v", err)
			}
			if got := buildCanonicalQueryString(req); got != tt.want {
				t.Errorf("buildCanonicalQueryString = %q, want %q", got, tt.want)
			}
		})
	}
}

// -------------------------------------------------------------------------
// AUTHENTICATION HAPPY PATH
// -------------------------------------------------------------------------

// signRequest computes a valid SigV4 signature for req using the same
// internal helpers as the verifier, then sets the Authorization header.
func signRequest(req *http.Request, accessKey, secret, scope string) {
	signedHeaders := []string{"host", "x-amz-content-sha256", "x-amz-date"}
	sort.Strings(signedHeaders)
	amzDate := req.Header.Get("X-Amz-Date")

	canonicalRequest := buildCanonicalRequest(req, signedHeaders)
	stringToSign := "AWS4-HMAC-SHA256\n" +
		amzDate + "\n" +
		scope + "\n" +
		sha256Hex([]byte(canonicalRequest))
	signingKey := deriveSigningKey(secret, scope)
	sig := hex.EncodeToString(hmacSHA256(signingKey, []byte(stringToSign)))

	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 "+
		"Credential="+accessKey+"/"+scope+", "+
		"SignedHeaders="+strings.Join(signedHeaders, ";")+", "+
		"Signature="+sig)
}

func TestAuthenticateAndResolveBucket_Valid(t *testing.T) {
	const (
		accessKey = "AKIDEXAMPLE"
		secret    = "wJalrXUtnFEMI"
		scope     = "20200101/us-east-1/s3/aws4_request"
	)
	r := NewBucketRegistry([]config.BucketConfig{{
		Name:        "photos",
		Credentials: []config.CredentialConfig{{AccessKeyID: accessKey, SecretAccessKey: secret}},
	}})

	req, _ := http.NewRequest(http.MethodGet, "/photos/cat.jpg?prefix=2020&max-keys=10", nil)
	req.Host = "s3.example.com"
	req.Header.Set("X-Amz-Date", time.Now().UTC().Format("20060102T150405Z"))
	req.Header.Set("X-Amz-Content-Sha256", "UNSIGNED-PAYLOAD")
	signRequest(req, accessKey, secret, scope)

	bucket, err := r.AuthenticateAndResolveBucket(req)
	if err != nil {
		t.Fatalf("AuthenticateAndResolveBucket err = %v, want nil", err)
	}
	if bucket != "photos" {
		t.Errorf("bucket = %q, want photos", bucket)
	}
}

func TestAuthenticateAndResolveBucket_BadSignature(t *testing.T) {
	r := NewBucketRegistry([]config.BucketConfig{{
		Name:        "photos",
		Credentials: []config.CredentialConfig{{AccessKeyID: "AKID", SecretAccessKey: "secret"}},
	}})

	req, _ := http.NewRequest(http.MethodGet, "/photos/cat.jpg", nil)
	req.Header.Set("X-Amz-Date", time.Now().UTC().Format("20060102T150405Z"))
	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 "+
		"Credential=AKID/20200101/us-east-1/s3/aws4_request, "+
		"SignedHeaders=host, Signature=deadbeef")

	if _, err := r.AuthenticateAndResolveBucket(req); err != ErrAccessDenied {
		t.Errorf("err = %v, want ErrAccessDenied", err)
	}
}

// -------------------------------------------------------------------------
// SIGNING KEY DERIVATION
// -------------------------------------------------------------------------

// TestDeriveSigningKey_ShortScope covers the fallback branch taken when the
// credential scope has fewer than three slash-separated parts.
func TestDeriveSigningKey_ShortScope(t *testing.T) {
	got := deriveSigningKey("secret", "onlyonepart")
	want := hmacSHA256([]byte("AWS4secret"), []byte("onlyonepart"))
	if !bytes.Equal(got, want) {
		t.Errorf("deriveSigningKey short scope = %x, want %x", got, want)
	}
}

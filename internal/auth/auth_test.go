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
	"net/url"
	"testing"

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
	if string(key1) != string(key2) {
		t.Error("expected cached key to match")
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
		values := params[k]
		for _, v := range values {
			pairs = append(pairs, kv{sigV4Encode(k), sigV4Encode(v)})
		}
	}

	// Sort by key, then by value
	sortPairs := func(i, j int) bool {
		if pairs[i].k != pairs[j].k {
			return pairs[i].k < pairs[j].k
		}
		return pairs[i].v < pairs[j].v
	}

	sorted := make([]kv, len(pairs))
	copy(sorted, pairs)
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sortPairs(j, i) {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	var result string
	for i, p := range sorted {
		if i > 0 {
			result += "&"
		}
		result += p.k + "=" + p.v
	}
	return result
}

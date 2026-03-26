// -------------------------------------------------------------------------------
// Server Helpers Tests
//
// Author: Alex Freidah
//
// Unit tests for path parsing, XML escaping, request ID validation, and
// user metadata extraction.
// -------------------------------------------------------------------------------

package server

import (
	"net/http"
	"testing"
)

// -------------------------------------------------------------------------
// PATH PARSING
// -------------------------------------------------------------------------

func TestParsePath(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		wantBucket string
		wantKey    string
		wantOK     bool
	}{
		{"root", "/", "", "", false},
		{"empty", "", "", "", false},
		{"bucket only", "/mybucket", "mybucket", "", true},
		{"bucket trailing slash", "/mybucket/", "mybucket", "", true},
		{"bucket and key", "/mybucket/mykey", "mybucket", "mykey", true},
		{"nested key", "/mybucket/path/to/object.bin", "mybucket", "path/to/object.bin", true},
		{"double slash", "//key", "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bucket, key, ok := parsePath(tt.path)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if bucket != tt.wantBucket {
				t.Errorf("bucket = %q, want %q", bucket, tt.wantBucket)
			}
			if key != tt.wantKey {
				t.Errorf("key = %q, want %q", key, tt.wantKey)
			}
		})
	}
}

// -------------------------------------------------------------------------
// REQUEST ID VALIDATION
// -------------------------------------------------------------------------

func TestIsValidRequestID(t *testing.T) {
	tests := []struct {
		name string
		id   string
		want bool
	}{
		{"empty", "", false},
		{"valid hex", "abcdef0123456789", true},
		{"uppercase hex", "ABCDEF0123456789", true},
		{"mixed case", "aAbBcC", true},
		{"non-hex chars", "xyz123", false},
		{"spaces", "abc 123", false},
		{"too long", string(make([]byte, 65)), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isValidRequestID(tt.id)
			if got != tt.want {
				t.Errorf("isValidRequestID(%q) = %v, want %v", tt.id, got, tt.want)
			}
		})
	}
}

// -------------------------------------------------------------------------
// XML ESCAPING
// -------------------------------------------------------------------------

func TestXmlEscape(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"hello", "hello"},
		{"<script>", "&lt;script&gt;"},
		{`"quoted"`, "&quot;quoted&quot;"},
		{"a&b", "a&amp;b"},
		{"it's", "it&apos;s"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := xmlEscape(tt.input)
			if got != tt.want {
				t.Errorf("xmlEscape(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// -------------------------------------------------------------------------
// USER METADATA EXTRACTION
// -------------------------------------------------------------------------

func TestExtractUserMetadata(t *testing.T) {
	h := http.Header{}
	h.Set("X-Amz-Meta-Author", "alex")
	h.Set("X-Amz-Meta-Project", "g3")
	h.Set("Content-Type", "text/plain")

	meta := extractUserMetadata(h)
	if len(meta) != 2 {
		t.Fatalf("len(meta) = %d, want 2", len(meta))
	}
	if meta["author"] != "alex" {
		t.Errorf("meta[author] = %q, want %q", meta["author"], "alex")
	}
	if meta["project"] != "g3" {
		t.Errorf("meta[project] = %q, want %q", meta["project"], "g3")
	}
}

func TestExtractUserMetadata_None(t *testing.T) {
	h := http.Header{}
	h.Set("Content-Type", "text/plain")

	meta := extractUserMetadata(h)
	if meta != nil {
		t.Errorf("expected nil, got %v", meta)
	}
}

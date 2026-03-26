// -------------------------------------------------------------------------------
// Search Query Builder Tests
//
// Author: Alex Freidah
//
// Unit tests for Gmail search query construction used in ListObjects,
// exact key lookup, and chunk discovery.
// -------------------------------------------------------------------------------

package backend

import (
	"strings"
	"testing"
)

// -------------------------------------------------------------------------
// LIST QUERY
// -------------------------------------------------------------------------

func TestBuildListQuery_NoPrefix(t *testing.T) {
	q := buildListQuery("s3", "mybucket", "")
	if !strings.Contains(q, "label:s3-mybucket") {
		t.Errorf("query missing label filter: %q", q)
	}
	if !strings.Contains(q, "subject:(s3://)") {
		t.Errorf("query missing subject filter: %q", q)
	}
	if !strings.Contains(q, "-subject:(#chunk-)") {
		t.Errorf("query missing chunk exclusion: %q", q)
	}
}

func TestBuildListQuery_WithPrefix(t *testing.T) {
	q := buildListQuery("s3", "mybucket", "photos/2024/")
	if !strings.Contains(q, "subject:(s3://mybucket/photos/2024/)") {
		t.Errorf("query missing prefix filter: %q", q)
	}
}

// -------------------------------------------------------------------------
// EXACT KEY QUERY
// -------------------------------------------------------------------------

func TestBuildExactKeyQuery(t *testing.T) {
	q := buildExactKeyQuery("s3", "mybucket", "path/to/file.txt")
	if !strings.Contains(q, "label:s3-mybucket") {
		t.Errorf("query missing label: %q", q)
	}
	if !strings.Contains(q, `subject:("s3://mybucket/path/to/file.txt")`) {
		t.Errorf("query missing exact subject: %q", q)
	}
}

// -------------------------------------------------------------------------
// CHUNK QUERY
// -------------------------------------------------------------------------

func TestBuildChunkQuery(t *testing.T) {
	q := buildChunkQuery("s3", "mybucket", "bigfile.tar.gz")
	if !strings.Contains(q, `subject:("s3://mybucket/bigfile.tar.gz#chunk-")`) {
		t.Errorf("query missing chunk prefix: %q", q)
	}
}

// -------------------------------------------------------------------------
// KEY EXTRACTION
// -------------------------------------------------------------------------

func TestExtractKeyFromSubject(t *testing.T) {
	tests := []struct {
		name    string
		subject string
		bucket  string
		want    string
	}{
		{"simple", "s3://mybucket/file.txt", "mybucket", "file.txt"},
		{"nested", "s3://mybucket/path/to/file.txt", "mybucket", "path/to/file.txt"},
		{"wrong bucket", "s3://other/file.txt", "mybucket", ""},
		{"no prefix", "random subject", "mybucket", ""},
		{"empty", "", "mybucket", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractKeyFromSubject(tt.subject, tt.bucket)
			if got != tt.want {
				t.Errorf("extractKeyFromSubject(%q, %q) = %q, want %q", tt.subject, tt.bucket, got, tt.want)
			}
		})
	}
}

// -------------------------------------------------------------------------
// SUBJECT FORMATTING
// -------------------------------------------------------------------------

func TestObjectSubject(t *testing.T) {
	got := objectSubject("mybucket", "path/to/key")
	want := "s3://mybucket/path/to/key"
	if got != want {
		t.Errorf("objectSubject = %q, want %q", got, want)
	}
}

func TestChunkSubject(t *testing.T) {
	got := chunkSubject("mybucket", "bigfile", 3)
	want := "s3://mybucket/bigfile#chunk-003"
	if got != want {
		t.Errorf("chunkSubject = %q, want %q", got, want)
	}
}

func TestLabelName(t *testing.T) {
	got := labelName("s3", "mybucket")
	want := "s3/mybucket"
	if got != want {
		t.Errorf("labelName = %q, want %q", got, want)
	}
}

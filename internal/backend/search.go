// -------------------------------------------------------------------------------
// Search Query Builder - Gmail Search Queries for S3 Operations
//
// Author: Alex Freidah
//
// Translates S3 ListObjects parameters (prefix, delimiter, start-after) into
// Gmail search queries. Gmail search supports label filtering and subject
// matching, which maps naturally to bucket isolation and key prefix filtering.
// -------------------------------------------------------------------------------

package backend

import (
	"fmt"
	"strings"
)

// -------------------------------------------------------------------------
// QUERY BUILDING
// -------------------------------------------------------------------------

// buildListQuery constructs a Gmail search query for listing objects in a
// bucket with an optional key prefix. The query uses label filtering for
// bucket isolation and subject matching for prefix filtering.
func buildListQuery(labelPrefix, bucket, prefix string) string {
	label := labelName(labelPrefix, bucket)
	q := fmt.Sprintf("label:%s", escapeLabel(label))

	// Filter to object emails (not chunks)
	q += " subject:(s3://)"

	// Exclude chunk emails from results
	q += " -subject:(#chunk-)"

	if prefix != "" {
		subjectPrefix := "s3://" + bucket + "/" + prefix
		q += fmt.Sprintf(" subject:(%s)", subjectPrefix)
	}

	return q
}

// buildExactKeyQuery constructs a Gmail search query to find the email for
// a specific object key. Uses exact subject matching.
func buildExactKeyQuery(labelPrefix, bucket, key string) string {
	label := labelName(labelPrefix, bucket)
	subject := objectSubject(bucket, key)
	return fmt.Sprintf("label:%s subject:(\"%s\")", escapeLabel(label), subject)
}

// buildChunkQuery constructs a Gmail search query to find all chunk emails
// for a chunked object.
func buildChunkQuery(labelPrefix, bucket, key string) string {
	label := labelName(labelPrefix, bucket)
	chunkPrefix := fmt.Sprintf("s3://%s/%s#chunk-", bucket, key)
	return fmt.Sprintf("label:%s subject:(\"%s\")", escapeLabel(label), chunkPrefix)
}

// -------------------------------------------------------------------------
// HELPERS
// -------------------------------------------------------------------------

// escapeLabel escapes forward slashes in label names for Gmail search syntax.
func escapeLabel(label string) string {
	return strings.ReplaceAll(label, "/", "-")
}

// extractKeyFromSubject parses the object key from an email subject line.
// Returns the key relative to the bucket (without the s3://bucket/ prefix).
func extractKeyFromSubject(subject, bucket string) string {
	prefix := "s3://" + bucket + "/"
	if strings.HasPrefix(subject, prefix) {
		return strings.TrimPrefix(subject, prefix)
	}
	return ""
}

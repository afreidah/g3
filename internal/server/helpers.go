// -------------------------------------------------------------------------------
// Server Helpers - S3 XML Responses, Error Writing, Path Parsing
//
// Author: Alex Freidah
//
// Shared utilities for the S3 HTTP handlers. Provides S3-compatible XML error
// responses, path parsing for bucket/key extraction, user metadata extraction
// from request headers, and request ID validation.
// -------------------------------------------------------------------------------

package server

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/afreidah/g3/internal/backend"
)

// headerContentType is the HTTP Content-Type header name.
const headerContentType = "Content-Type"

// -------------------------------------------------------------------------
// S3 ERROR RESPONSES
// -------------------------------------------------------------------------

// writeS3Error writes an S3-compatible XML error response.
func writeS3Error(w http.ResponseWriter, code int, errCode, message string) {
	body := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<Error>
  <Code>%s</Code>
  <Message>%s</Message>
</Error>`, xmlEscape(errCode), xmlEscape(message))
	w.Header().Set("Content-Type", "application/xml")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(code)
	_, _ = io.WriteString(w, body)
}

// writeStorageError maps a backend error to an S3 XML error response. Typed
// S3Error instances are mapped directly; untyped errors fall back to 502.
func writeStorageError(w http.ResponseWriter, err error) int {
	var s3err *backend.S3Error
	if errors.As(err, &s3err) {
		writeS3Error(w, s3err.StatusCode, s3err.Code, s3err.Message)
		return s3err.StatusCode
	}
	writeS3Error(w, http.StatusBadGateway, "InternalError", "An internal error occurred")
	return http.StatusBadGateway
}

// -------------------------------------------------------------------------
// XML HELPERS
// -------------------------------------------------------------------------

var xmlReplacer = strings.NewReplacer(
	"&", "&amp;",
	"<", "&lt;",
	">", "&gt;",
	"\"", "&quot;",
	"'", "&apos;",
)

// xmlEscape escapes the five XML special characters.
func xmlEscape(s string) string {
	return xmlReplacer.Replace(s)
}

// -------------------------------------------------------------------------
// PATH PARSING
// -------------------------------------------------------------------------

// parsePath extracts the bucket and key from the URL path. Returns false if
// the path does not contain a valid bucket name.
func parsePath(path string) (bucket string, key string, ok bool) {
	path = strings.TrimPrefix(path, "/")
	if path == "" {
		return "", "", false
	}
	parts := strings.SplitN(path, "/", 2)
	if parts[0] == "" {
		return "", "", false
	}
	if len(parts) == 1 || parts[1] == "" {
		return parts[0], "", true
	}
	return parts[0], parts[1], true
}

// -------------------------------------------------------------------------
// METADATA HELPERS
// -------------------------------------------------------------------------

// extractUserMetadata reads x-amz-meta-* headers from the request and
// returns them as a map with lowercased keys (without the prefix).
func extractUserMetadata(h http.Header) map[string]string {
	var meta map[string]string
	for k, vals := range h {
		lower := strings.ToLower(k)
		if strings.HasPrefix(lower, "x-amz-meta-") {
			if meta == nil {
				meta = make(map[string]string)
			}
			meta[strings.TrimPrefix(lower, "x-amz-meta-")] = vals[0]
		}
	}
	return meta
}

// -------------------------------------------------------------------------
// REQUEST ID VALIDATION
// -------------------------------------------------------------------------

// isValidRequestID checks that an X-Request-Id value is a hex string of
// reasonable length.
func isValidRequestID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for _, c := range id {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return true
}

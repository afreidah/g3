// -------------------------------------------------------------------------------
// List Handler - ListObjectsV2
//
// Author: Alex Freidah
//
// Implements the S3 ListObjectsV2 API by parsing query parameters and
// delegating to the backend. Returns an XML response with object keys,
// sizes, common prefixes, and pagination tokens.
// -------------------------------------------------------------------------------

package server

import (
	"context"
	"encoding/xml"
	"io"
	"net/http"
	"strconv"
	"time"
)

// -------------------------------------------------------------------------
// XML TYPES
// -------------------------------------------------------------------------

// listBucketResult is the XML response for ListObjectsV2.
type listBucketResult struct {
	XMLName               xml.Name       `xml:"ListBucketResult"`
	XMLNS                 string         `xml:"xmlns,attr"`
	Name                  string         `xml:"Name"`
	Prefix                string         `xml:"Prefix"`
	Delimiter             string         `xml:"Delimiter,omitempty"`
	MaxKeys               int            `xml:"MaxKeys"`
	KeyCount              int            `xml:"KeyCount"`
	IsTruncated           bool           `xml:"IsTruncated"`
	NextContinuationToken string         `xml:"NextContinuationToken,omitempty"`
	Contents              []listObject   `xml:"Contents"`
	CommonPrefixes        []commonPrefix `xml:"CommonPrefixes,omitempty"`
}

type listObject struct {
	Key          string `xml:"Key"`
	Size         int64  `xml:"Size"`
	LastModified string `xml:"LastModified"`
	ETag         string `xml:"ETag,omitempty"`
}

type commonPrefix struct {
	Prefix string `xml:"Prefix"`
}

// -------------------------------------------------------------------------
// LIST OBJECTS V2
// -------------------------------------------------------------------------

// handleListObjects processes an S3 ListObjectsV2 request. Parses prefix,
// delimiter, max-keys, and start-after from query parameters and delegates
// to the backend.
func (s *Server) handleListObjects(ctx context.Context, w http.ResponseWriter, r *http.Request, bucket string) (int, error) {
	q := r.URL.Query()
	prefix := q.Get("prefix")
	delimiter := q.Get("delimiter")
	startAfter := q.Get("start-after")

	maxKeys := 1000
	if mk := q.Get("max-keys"); mk != "" {
		if parsed, err := strconv.Atoi(mk); err == nil && parsed > 0 {
			maxKeys = parsed
		}
	}

	// continuation-token is treated as start-after
	if ct := q.Get("continuation-token"); ct != "" {
		startAfter = ct
	}

	result, err := s.backend.ListObjects(ctx, bucket, prefix, delimiter, startAfter, maxKeys)
	if err != nil {
		status := writeStorageError(w, err)
		return status, err
	}

	xmlResult := listBucketResult{
		XMLNS:       "http://s3.amazonaws.com/doc/2006-03-01/",
		Name:        bucket,
		Prefix:      prefix,
		Delimiter:   delimiter,
		MaxKeys:     maxKeys,
		KeyCount:    len(result.Contents),
		IsTruncated: result.IsTruncated,
	}

	if result.IsTruncated && result.NextStartAfter != "" {
		xmlResult.NextContinuationToken = result.NextStartAfter
	}

	for _, obj := range result.Contents {
		xmlResult.Contents = append(xmlResult.Contents, listObject{
			Key:          obj.Key,
			Size:         obj.Size,
			LastModified: obj.LastModified.UTC().Format(time.RFC3339),
			ETag:         obj.ETag,
		})
	}

	for _, cp := range result.CommonPrefixes {
		xmlResult.CommonPrefixes = append(xmlResult.CommonPrefixes, commonPrefix{Prefix: cp})
	}

	body, err := xml.Marshal(xmlResult)
	if err != nil {
		writeS3Error(w, http.StatusInternalServerError, "InternalError", "Failed to serialize response")
		return http.StatusInternalServerError, err
	}

	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, xml.Header)
	_, _ = w.Write(body)
	return http.StatusOK, nil
}

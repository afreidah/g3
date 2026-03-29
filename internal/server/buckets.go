// -------------------------------------------------------------------------------
// Bucket Handlers - ListBuckets and CreateBucket
//
// Author: Alex Freidah
//
// Handles S3 bucket-level operations. ListBuckets returns all Gmail labels
// matching the configured prefix as S3 buckets. CreateBucket creates a new
// Gmail label.
// -------------------------------------------------------------------------------

package server

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"time"
)

// -------------------------------------------------------------------------
// XML TYPES
// -------------------------------------------------------------------------

// listAllMyBucketsResult is the XML response for ListBuckets.
type listAllMyBucketsResult struct {
	XMLName xml.Name     `xml:"ListAllMyBucketsResult"`
	XMLNS   string       `xml:"xmlns,attr"`
	Owner   bucketOwner  `xml:"Owner"`
	Buckets bucketList   `xml:"Buckets"`
}

type bucketOwner struct {
	ID          string `xml:"ID"`
	DisplayName string `xml:"DisplayName"`
}

type bucketList struct {
	Bucket []bucketEntry `xml:"Bucket"`
}

type bucketEntry struct {
	Name         string `xml:"Name"`
	CreationDate string `xml:"CreationDate"`
}

// -------------------------------------------------------------------------
// LIST BUCKETS
// -------------------------------------------------------------------------

// handleListBuckets queries the backend for all buckets and returns an S3
// ListAllMyBucketsResult XML response.
func (s *Server) handleListBuckets(ctx context.Context, w http.ResponseWriter) int {
	buckets, err := s.backend.ListBuckets(ctx)
	if err != nil {
		return writeStorageError(w, err)
	}

	result := listAllMyBucketsResult{
		XMLNS: "http://s3.amazonaws.com/doc/2006-03-01/",
		Owner: bucketOwner{
			ID:          "g3",
			DisplayName: "g3",
		},
	}

	for _, b := range buckets {
		cd := b.CreationDate
		if cd.IsZero() {
			cd = time.Now().UTC()
		}
		result.Buckets.Bucket = append(result.Buckets.Bucket, bucketEntry{
			Name:         b.Name,
			CreationDate: cd.Format(time.RFC3339),
		})
	}

	body, err := xml.Marshal(result)
	if err != nil {
		writeS3Error(w, http.StatusInternalServerError, "InternalError", "Failed to serialize response")
		return http.StatusInternalServerError
	}

	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, xml.Header)
	_, _ = w.Write(body)
	return http.StatusOK
}

// -------------------------------------------------------------------------
// HEAD BUCKET
// -------------------------------------------------------------------------

// handleHeadBucket checks if a bucket exists by attempting to list its label.
func (s *Server) handleHeadBucket(ctx context.Context, w http.ResponseWriter, bucket string) (int, error) {
	buckets, err := s.backend.ListBuckets(ctx)
	if err != nil {
		status := writeStorageError(w, err)
		return status, err
	}

	for _, b := range buckets {
		if b.Name == bucket {
			w.WriteHeader(http.StatusOK)
			return http.StatusOK, nil
		}
	}

	writeS3Error(w, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist")
	return http.StatusNotFound, nil
}

// -------------------------------------------------------------------------
// GET BUCKET LOCATION
// -------------------------------------------------------------------------

// handleGetBucketLocation returns a stub location response. Gmail has no
// concept of regions, so this always returns an empty LocationConstraint
// which the aws CLI interprets as us-east-1.
func (s *Server) handleGetBucketLocation(ctx context.Context, w http.ResponseWriter) (int, error) {
	body := `<?xml version="1.0" encoding="UTF-8"?>
<LocationConstraint xmlns="http://s3.amazonaws.com/doc/2006-03-01/"/>`
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, body)
	return http.StatusOK, nil
}

// -------------------------------------------------------------------------
// CREATE BUCKET
// -------------------------------------------------------------------------

// handleCreateBucket creates a new bucket (Gmail label) via the backend.
func (s *Server) handleCreateBucket(ctx context.Context, w http.ResponseWriter, bucket string) (int, error) {
	err := s.backend.CreateBucket(ctx, bucket)
	if err != nil {
		status := writeStorageError(w, err)
		return status, err
	}

	w.Header().Set("Location", fmt.Sprintf("/%s", bucket))
	w.WriteHeader(http.StatusOK)
	return http.StatusOK, nil
}

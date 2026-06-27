// -------------------------------------------------------------------------------
// Multipart Upload - In-Memory Part Buffering with Streaming Assembly
//
// Author: Alex Freidah
//
// Accepts the S3 multipart upload API by buffering individual parts in memory.
// On CompleteMultipartUpload, parts are streamed in order via io.MultiReader
// into the backend's PutObject without copying into a single buffer. Abandoned
// uploads are cleaned up by a TTL-based expiry.
// -------------------------------------------------------------------------------

package server

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/xml"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/afreidah/g3/internal/audit"
	"github.com/afreidah/g3/internal/telemetry"
)

// -------------------------------------------------------------------------
// MULTIPART STORE
// -------------------------------------------------------------------------

// multipartUpload tracks an in-progress multipart upload.
type multipartUpload struct {
	bucket      string
	key         string
	contentType string
	metadata    map[string]string
	parts       map[int][]byte
	createdAt   time.Time
}

const (
	maxConcurrentUploads = 100
	maxPartNumber        = 10000
)

// MultipartStore manages in-progress multipart uploads in memory.
type MultipartStore struct {
	mu      sync.RWMutex
	uploads map[string]*multipartUpload
	ttl     time.Duration
}

// NewMultipartStore creates a store with the given TTL for abandoned uploads.
func NewMultipartStore(ttl time.Duration) *MultipartStore {
	return &MultipartStore{
		uploads: make(map[string]*multipartUpload),
		ttl:     ttl,
	}
}

// create starts a new multipart upload and returns the upload ID. Returns
// an empty string if the maximum number of concurrent uploads is reached.
func (s *MultipartStore) create(bucket, key, contentType string, metadata map[string]string) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.uploads) >= maxConcurrentUploads {
		return ""
	}

	uploadID := audit.NewID()
	telemetry.MultipartUploadsCreatedTotal.Inc()
	telemetry.MultipartUploadsActive.Inc()
	s.uploads[uploadID] = &multipartUpload{
		bucket:      bucket,
		key:         key,
		contentType: contentType,
		metadata:    metadata,
		parts:       make(map[int][]byte),
		createdAt:   time.Now(),
	}
	return uploadID
}

// addPart stores a part for the given upload ID. Returns the ETag (MD5) of
// the part data. Part numbers must be between 1 and 10000 per S3 spec.
func (s *MultipartStore) addPart(uploadID string, partNumber int, data []byte) (string, error) {
	if partNumber < 1 || partNumber > maxPartNumber {
		return "", fmt.Errorf("part number %d out of range (1-%d)", partNumber, maxPartNumber)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	upload, ok := s.uploads[uploadID]
	if !ok {
		return "", fmt.Errorf("upload %s not found", uploadID)
	}

	upload.parts[partNumber] = data
	hash := md5.Sum(data)
	return fmt.Sprintf("%x", hash), nil
}

// complete returns a streaming reader over the assembled parts in order.
// Uses io.MultiReader to avoid copying all parts into a single buffer.
// Removes the upload from the store.
func (s *MultipartStore) complete(uploadID string) (*multipartUpload, io.Reader, int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	upload, ok := s.uploads[uploadID]
	if !ok {
		return nil, nil, 0, fmt.Errorf("upload %s not found", uploadID)
	}

	// Sort part numbers
	partNums := make([]int, 0, len(upload.parts))
	for n := range upload.parts {
		partNums = append(partNums, n)
	}
	sort.Ints(partNums)

	// Build sequential reader over parts without copying
	readers := make([]io.Reader, 0, len(partNums))
	var totalSize int64
	for _, n := range partNums {
		readers = append(readers, bytes.NewReader(upload.parts[n]))
		totalSize += int64(len(upload.parts[n]))
	}

	delete(s.uploads, uploadID)
	telemetry.MultipartUploadsCompletedTotal.Inc()
	telemetry.MultipartUploadsActive.Dec()
	return upload, io.MultiReader(readers...), totalSize, nil
}

// abort discards an in-progress upload.
func (s *MultipartStore) abort(uploadID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.uploads[uploadID]; ok {
		delete(s.uploads, uploadID)
		telemetry.MultipartUploadsAbortedTotal.Inc()
		telemetry.MultipartUploadsActive.Dec()
	}
}

// CleanExpired removes uploads older than the configured TTL.
func (s *MultipartStore) CleanExpired() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	cutoff := time.Now().Add(-s.ttl)
	removed := 0
	for id, upload := range s.uploads {
		if upload.createdAt.Before(cutoff) {
			delete(s.uploads, id)
			telemetry.MultipartUploadsExpiredTotal.Inc()
			telemetry.MultipartUploadsActive.Dec()
			removed++
		}
	}
	return removed
}

// StartCleanupLoop runs a background goroutine that periodically removes
// expired uploads. Stops when the context is cancelled.
func (s *MultipartStore) StartCleanupLoop(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if n := s.CleanExpired(); n > 0 {
					slog.InfoContext(ctx, "Cleaned expired multipart uploads", "count", n)
				}
			}
		}
	}()
}

// -------------------------------------------------------------------------
// XML TYPES
// -------------------------------------------------------------------------

type initiateMultipartUploadResult struct {
	XMLName  xml.Name `xml:"InitiateMultipartUploadResult"`
	XMLNS    string   `xml:"xmlns,attr"`
	Bucket   string   `xml:"Bucket"`
	Key      string   `xml:"Key"`
	UploadId string   `xml:"UploadId"`
}

type completeMultipartUploadResult struct {
	XMLName xml.Name `xml:"CompleteMultipartUploadResult"`
	XMLNS   string   `xml:"xmlns,attr"`
	Bucket  string   `xml:"Bucket"`
	Key     string   `xml:"Key"`
	ETag    string   `xml:"ETag"`
}

// -------------------------------------------------------------------------
// HANDLERS
// -------------------------------------------------------------------------

// handleCreateMultipartUpload starts a new multipart upload.
func (s *Server) handleCreateMultipartUpload(ctx context.Context, w http.ResponseWriter, r *http.Request, bucket, key string) (int, error) {
	contentType := r.Header.Get(headerContentType)
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	metadata := extractUserMetadata(r.Header)

	uploadID := s.multipart.create(bucket, key, contentType, metadata)
	if uploadID == "" {
		writeS3Error(w, http.StatusServiceUnavailable, "SlowDown", "Too many concurrent uploads")
		return http.StatusServiceUnavailable, nil
	}

	result := initiateMultipartUploadResult{
		XMLNS:    "http://s3.amazonaws.com/doc/2006-03-01/",
		Bucket:   bucket,
		Key:      key,
		UploadId: uploadID,
	}

	body, err := xml.Marshal(result)
	if err != nil {
		writeS3Error(w, http.StatusInternalServerError, "InternalError", "Failed to serialize response")
		return http.StatusInternalServerError, err
	}

	w.Header().Set(headerContentType, "application/xml")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, xml.Header)
	_, _ = w.Write(body)
	return http.StatusOK, nil
}

// handleUploadPart stores a part for an in-progress multipart upload.
func (s *Server) handleUploadPart(ctx context.Context, w http.ResponseWriter, r *http.Request, bucket, key string) (int, int64, error) {
	uploadID := r.URL.Query().Get("uploadId")
	partNumStr := r.URL.Query().Get("partNumber")

	partNum, err := strconv.Atoi(partNumStr)
	if err != nil || partNum < 1 {
		writeS3Error(w, http.StatusBadRequest, "InvalidArgument", "Invalid part number")
		return http.StatusBadRequest, 0, nil
	}

	data, err := io.ReadAll(r.Body)
	if err != nil {
		writeS3Error(w, http.StatusInternalServerError, "InternalError", "Failed to read part data")
		return http.StatusInternalServerError, 0, err
	}

	etag, err := s.multipart.addPart(uploadID, partNum, data)
	if err != nil {
		writeS3Error(w, http.StatusNotFound, "NoSuchUpload", "The specified upload does not exist")
		return http.StatusNotFound, 0, err
	}

	w.Header().Set("ETag", `"`+etag+`"`)
	w.WriteHeader(http.StatusOK)
	return http.StatusOK, int64(len(data)), nil
}

// handleCompleteMultipartUpload assembles parts and writes the object.
func (s *Server) handleCompleteMultipartUpload(ctx context.Context, w http.ResponseWriter, r *http.Request, bucket, key string) (int, error) {
	uploadID := r.URL.Query().Get("uploadId")

	// Read and discard the completion XML (we don't validate part ETags)
	_, _ = io.ReadAll(io.LimitReader(r.Body, 1<<20))

	upload, reader, size, err := s.multipart.complete(uploadID)
	if err != nil {
		writeS3Error(w, http.StatusNotFound, "NoSuchUpload", "The specified upload does not exist")
		return http.StatusNotFound, err
	}

	etag, err := s.backend.PutObject(ctx, bucket, key, reader, size, upload.contentType, upload.metadata)
	if err != nil {
		status := writeStorageError(w, err)
		return status, err
	}

	result := completeMultipartUploadResult{
		XMLNS:  "http://s3.amazonaws.com/doc/2006-03-01/",
		Bucket: bucket,
		Key:    key,
		ETag:   `"` + etag + `"`,
	}

	body, err := xml.Marshal(result)
	if err != nil {
		writeS3Error(w, http.StatusInternalServerError, "InternalError", "Failed to serialize response")
		return http.StatusInternalServerError, err
	}

	w.Header().Set(headerContentType, "application/xml")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, xml.Header)
	_, _ = w.Write(body)
	return http.StatusOK, nil
}

// handleAbortMultipartUpload discards an in-progress upload.
func (s *Server) handleAbortMultipartUpload(ctx context.Context, w http.ResponseWriter, r *http.Request) (int, error) {
	uploadID := r.URL.Query().Get("uploadId")
	s.multipart.abort(uploadID)
	w.WriteHeader(http.StatusNoContent)
	return http.StatusNoContent, nil
}

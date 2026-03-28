// -------------------------------------------------------------------------------
// Multipart Upload - In-Memory Part Buffering and Assembly
//
// Author: Alex Freidah
//
// Accepts the S3 multipart upload API by buffering parts in memory. On
// CompleteMultipartUpload, parts are assembled into a single object and
// delegated to the backend's PutObject (which handles chunking for large
// objects). Abandoned uploads are cleaned up by a TTL-based expiry.
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

// create starts a new multipart upload and returns the upload ID.
func (s *MultipartStore) create(bucket, key, contentType string, metadata map[string]string) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	uploadID := audit.NewID()
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
// the part data.
func (s *MultipartStore) addPart(uploadID string, partNumber int, data []byte) (string, error) {
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

// complete assembles all parts in order and returns the upload metadata and
// assembled data. Removes the upload from the store.
func (s *MultipartStore) complete(uploadID string) (*multipartUpload, []byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	upload, ok := s.uploads[uploadID]
	if !ok {
		return nil, nil, fmt.Errorf("upload %s not found", uploadID)
	}

	// Sort part numbers
	partNums := make([]int, 0, len(upload.parts))
	for n := range upload.parts {
		partNums = append(partNums, n)
	}
	sort.Ints(partNums)

	// Assemble
	var buf bytes.Buffer
	for _, n := range partNums {
		buf.Write(upload.parts[n])
	}

	delete(s.uploads, uploadID)
	return upload, buf.Bytes(), nil
}

// abort discards an in-progress upload.
func (s *MultipartStore) abort(uploadID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.uploads, uploadID)
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
	contentType := r.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	metadata := extractUserMetadata(r.Header)

	uploadID := s.multipart.create(bucket, key, contentType, metadata)

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

	w.Header().Set("Content-Type", "application/xml")
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

	upload, data, err := s.multipart.complete(uploadID)
	if err != nil {
		writeS3Error(w, http.StatusNotFound, "NoSuchUpload", "The specified upload does not exist")
		return http.StatusNotFound, err
	}

	etag, err := s.backend.PutObject(ctx, bucket, key, bytes.NewReader(data), int64(len(data)), upload.contentType, upload.metadata)
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

	w.Header().Set("Content-Type", "application/xml")
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

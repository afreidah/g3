// -------------------------------------------------------------------------------
// Gmail Backend - S3 Object Operations via Gmail API
//
// Author: Alex Freidah
//
// Implements the ObjectBackend interface using Gmail as the storage layer.
// Each S3 object is stored as an email with a JSON metadata body and a binary
// attachment. Bucket isolation is achieved via Gmail labels. All Gmail API
// calls are instrumented with OpenTelemetry client spans and Prometheus
// metrics.
// -------------------------------------------------------------------------------

package backend

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/afreidah/g3/internal/config"
	"github.com/afreidah/g3/internal/telemetry"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"
)

// -------------------------------------------------------------------------
// GMAIL BACKEND
// -------------------------------------------------------------------------

// GmailBackend implements ObjectBackend using Gmail for metadata and Google
// Drive for object data storage.
type GmailBackend struct {
	gmail         *gmail.Service
	drive         *drive.Service
	store         MetadataStore
	user          string
	labelPrefix   string
	driveFolderID string
	chunkSize     int64 // legacy: only used for reading old chunked objects

	// labelCache maps bucket name to Gmail label ID
	labelMu    sync.RWMutex
	labelCache map[string]string
}

// MetadataStore is the interface for the local metadata index. Decouples
// the backend from the concrete SQLite implementation for testability.
type MetadataStore interface {
	PutObject(ctx context.Context, rec *ObjectRecord) error
	GetObject(ctx context.Context, bucket, key string) (*ObjectRecord, error)
	DeleteObject(ctx context.Context, bucket, key string) error
	ListObjects(ctx context.Context, bucket, prefix, startAfter string, maxKeys int) ([]*ObjectRecord, error)
	PutBucket(ctx context.Context, rec *BucketRecord) error
	GetBucket(ctx context.Context, name string) (*BucketRecord, error)
	ListBuckets(ctx context.Context) ([]*BucketRecord, error)
}

// ObjectRecord represents a stored object's metadata and Gmail reference.
type ObjectRecord struct {
	Bucket      string
	Key         string
	GmailMsgID  string
	DriveFileID string
	ETag        string
	Size        int64
	ContentType string
	CreatedAt   time.Time
	Metadata    map[string]string
}

// BucketRecord represents a stored bucket's label mapping.
type BucketRecord struct {
	Name      string
	LabelID   string
	CreatedAt time.Time
}

// NewGmailBackend creates a GmailBackend from the provided configuration.
// It builds an OAuth2 token source from the configured client credentials
// and refresh token, then initializes the Gmail API client.
func NewGmailBackend(ctx context.Context, cfg *config.GmailConfig, store MetadataStore) (*GmailBackend, error) { // codecov:ignore -- requires Gmail API credentials
	oauthCfg := &oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		Endpoint:     google.Endpoint,
		Scopes:       []string{gmail.GmailModifyScope, drive.DriveFileScope},
	}

	tok := &oauth2.Token{RefreshToken: cfg.RefreshToken}
	client := oauthCfg.Client(ctx, tok)

	gmailSvc, err := gmail.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		return nil, fmt.Errorf("create gmail service: %w", err)
	}

	driveSvc, err := drive.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		return nil, fmt.Errorf("create drive service: %w", err)
	}

	// Ensure root folder exists for g3 objects
	folderID, err := ensureDriveFolder(ctx, driveSvc, cfg.LabelPrefix)
	if err != nil {
		return nil, fmt.Errorf("ensure drive folder: %w", err)
	}

	return &GmailBackend{
		gmail:         gmailSvc,
		drive:         driveSvc,
		store:         store,
		user:          cfg.User,
		labelPrefix:   cfg.LabelPrefix,
		driveFolderID: folderID,
		chunkSize:     cfg.ChunkSizeBytes,
		labelCache:    make(map[string]string),
	}, nil
}

// ensureDriveFolder finds or creates a root folder in Drive for g3 objects.
func ensureDriveFolder(ctx context.Context, svc *drive.Service, name string) (string, error) { // codecov:ignore -- requires Drive API
	// Search for existing folder
	query := fmt.Sprintf("name='%s' and mimeType='application/vnd.google-apps.folder' and trashed=false", name)
	list, err := svc.Files.List().Q(query).Fields("files(id)").Context(ctx).Do()
	if err != nil {
		return "", err
	}
	if len(list.Files) > 0 {
		return list.Files[0].Id, nil
	}

	// Create folder
	folder, err := svc.Files.Create(&drive.File{
		Name:     name,
		MimeType: "application/vnd.google-apps.folder",
	}).Fields("id").Context(ctx).Do()
	if err != nil {
		return "", err
	}
	return folder.Id, nil
}

// -------------------------------------------------------------------------
// OBJECT OPERATIONS
// -------------------------------------------------------------------------

// PutObject uploads object data to Google Drive and stores a metadata-only
// email in Gmail as a pointer. If an object with the same key exists, it is
// deleted first (last-write-wins semantics).
func (g *GmailBackend) PutObject(ctx context.Context, bucket, key string, body io.Reader, size int64, contentType string, metadata map[string]string) (string, error) { // codecov:ignore -- requires Gmail/Drive API
	start := time.Now()
	ctx, span := telemetry.StartClientSpan(ctx, "Drive.Upload",
		telemetry.GmailAttributes("PutObject", bucket, key)...,
	)
	defer span.End()

	// Read object data and compute ETag
	data, err := io.ReadAll(body)
	if err != nil {
		g.recordOp("PutObject", start, err)
		return "", fmt.Errorf("read body: %w", err)
	}

	hash := md5.Sum(data)
	etag := fmt.Sprintf("%x", hash)

	if contentType == "" {
		contentType = "application/octet-stream"
	}

	// Delete existing object if present (last-write-wins)
	g.deleteExisting(ctx, bucket, key)

	// Resolve label ID for bucket
	labelID, err := g.resolveLabelID(ctx, bucket)
	if err != nil {
		g.recordOp("PutObject", start, err)
		return "", fmt.Errorf("resolve label: %w", err)
	}

	// Upload data to Google Drive
	driveFile, err := g.drive.Files.Create(&drive.File{
		Name:    bucket + "/" + key,
		Parents: []string{g.driveFolderID},
	}).Media(bytes.NewReader(data)).Fields("id").Context(ctx).Do()
	if err != nil {
		g.recordOp("PutObject", start, err)
		return "", fmt.Errorf("drive upload: %w", err)
	}

	// Insert metadata-only email in Gmail
	meta := &objectMetadata{
		ContentType: contentType,
		ETag:        etag,
		Size:        int64(len(data)),
		Metadata:    metadata,
		DriveFileID: driveFile.Id,
		CreatedAt:   time.Now().UTC(),
	}
	subject := objectSubject(bucket, key)
	rawEmail, err := buildObjectEmail(subject, meta, nil)
	if err != nil {
		g.recordOp("PutObject", start, err)
		return "", fmt.Errorf("build email: %w", err)
	}

	msg := &gmail.Message{
		Raw:      base64.URLEncoding.EncodeToString(rawEmail),
		LabelIds: []string{labelID},
	}
	sent, err := g.gmail.Users.Messages.Insert(g.user, msg).
		InternalDateSource("dateHeader").
		Context(ctx).
		Do()
	if err != nil {
		// Clean up Drive file if Gmail insert fails
		_ = g.drive.Files.Delete(driveFile.Id).Context(ctx).Do()
		g.recordOp("PutObject", start, err)
		return "", fmt.Errorf("gmail insert: %w", err)
	}

	span.SetAttributes(telemetry.AttrGmailMsgID.String(sent.Id))

	// Record in metadata store
	if g.store != nil {
		_ = g.store.PutObject(ctx, &ObjectRecord{
			Bucket:      bucket,
			Key:         key,
			GmailMsgID:  sent.Id,
			DriveFileID: driveFile.Id,
			ETag:        etag,
			Size:        int64(len(data)),
			ContentType: contentType,
			CreatedAt:   time.Now().UTC(),
			Metadata:    metadata,
		})
	}

	slog.InfoContext(ctx, "Object stored",
		"bucket", bucket, "key", key, "size", len(data),
		"gmail_id", sent.Id, "drive_id", driveFile.Id,
	)

	g.recordOp("PutObject", start, nil)
	return etag, nil
}

// GetObject retrieves object data from Google Drive using the file ID stored
// in the local index or Gmail email metadata.
func (g *GmailBackend) GetObject(ctx context.Context, bucket, key string) (*GetObjectResult, error) { // codecov:ignore -- requires Gmail/Drive API
	start := time.Now()
	ctx, span := telemetry.StartClientSpan(ctx, "Drive.Download",
		telemetry.GmailAttributes("GetObject", bucket, key)...,
	)
	defer span.End()

	// Look up metadata from store or Gmail
	var rec *ObjectRecord
	var driveFileID string
	var objSize int64
	var objContentType, objETag string
	var objCreatedAt time.Time
	var objMetadata map[string]string

	if g.store != nil {
		r, err := g.store.GetObject(ctx, bucket, key)
		if err == nil && r != nil {
			rec = r
		}
	}

	if rec != nil && rec.DriveFileID != "" {
		driveFileID = rec.DriveFileID
		objSize = rec.Size
		objContentType = rec.ContentType
		objETag = rec.ETag
		objCreatedAt = rec.CreatedAt
		objMetadata = rec.Metadata
	} else {
		// Fallback: fetch metadata from Gmail
		meta, err := g.fetchMetadataOnly(ctx, bucket, key)
		if err != nil {
			g.recordOp("GetObject", start, err)
			return nil, err
		}
		driveFileID = meta.DriveFileID
		objSize = meta.Size
		objContentType = meta.ContentType
		objETag = meta.ETag
		objCreatedAt = meta.CreatedAt
		objMetadata = meta.Metadata

		if driveFileID == "" {
			// Legacy attachment-based object — fall back to old path
			var data []byte
			var fetchErr error
			if meta.Chunked {
				data, fetchErr = g.getChunked(ctx, bucket, key, meta)
			} else {
				_, data, fetchErr = g.fetchObject(ctx, bucket, key)
			}
			if fetchErr != nil {
				g.recordOp("GetObject", start, fetchErr)
				return nil, fetchErr
			}
			g.recordOp("GetObject", start, nil)
			return &GetObjectResult{
				Body:         io.NopCloser(bytes.NewReader(data)),
				Size:         meta.Size,
				ContentType:  meta.ContentType,
				ETag:         meta.ETag,
				LastModified: meta.CreatedAt,
				Metadata:     meta.Metadata,
			}, nil
		}
	}

	// Download from Drive
	resp, err := g.drive.Files.Get(driveFileID).Download()
	if err != nil {
		g.recordOp("GetObject", start, err)
		return nil, fmt.Errorf("drive download: %w", err)
	}

	g.recordOp("GetObject", start, nil)
	return &GetObjectResult{
		Body:         resp.Body,
		Size:         objSize,
		ContentType:  objContentType,
		ETag:         objETag,
		LastModified: objCreatedAt,
		Metadata:     objMetadata,
	}, nil
}

// HeadObject retrieves only the metadata for an object. Checks the local
// SQLite index first (zero API calls). Falls back to Gmail API on cache miss.
func (g *GmailBackend) HeadObject(ctx context.Context, bucket, key string) (*HeadObjectResult, error) { // codecov:ignore -- requires Gmail API
	start := time.Now()
	ctx, span := telemetry.StartClientSpan(ctx, "Gmail.GetMessage",
		telemetry.GmailAttributes("HeadObject", bucket, key)...,
	)
	defer span.End()

	// Check local store first
	if g.store != nil {
		rec, err := g.store.GetObject(ctx, bucket, key)
		if err == nil && rec != nil {
			g.recordOp("HeadObject", start, nil)
			return &HeadObjectResult{
				Size:         rec.Size,
				ContentType:  rec.ContentType,
				ETag:         rec.ETag,
				LastModified: rec.CreatedAt,
				Metadata:     rec.Metadata,
			}, nil
		}
	}

	// Fallback to Gmail API
	meta, err := g.fetchMetadataOnly(ctx, bucket, key)
	if err != nil {
		g.recordOp("HeadObject", start, err)
		return nil, err
	}

	g.recordOp("HeadObject", start, nil)
	return &HeadObjectResult{
		Size:         meta.Size,
		ContentType:  meta.ContentType,
		ETag:         meta.ETag,
		LastModified: meta.CreatedAt,
		Metadata:     meta.Metadata,
	}, nil
}

// DeleteObject removes an object by deleting its Drive file, Gmail message,
// and metadata store record. Returns nil if the object does not exist (S3
// idempotency).
func (g *GmailBackend) DeleteObject(ctx context.Context, bucket, key string) error { // codecov:ignore -- requires Gmail/Drive API
	start := time.Now()
	ctx, span := telemetry.StartClientSpan(ctx, "Gmail.DeleteMessage",
		telemetry.GmailAttributes("DeleteObject", bucket, key)...,
	)
	defer span.End()

	g.deleteExisting(ctx, bucket, key)

	g.recordOp("DeleteObject", start, nil)
	return nil
}

// -------------------------------------------------------------------------
// INTERNAL HELPERS
// -------------------------------------------------------------------------

// deleteExisting removes all traces of an object — Drive file, Gmail
// message, legacy chunks, and metadata store record.
func (g *GmailBackend) deleteExisting(ctx context.Context, bucket, key string) { // codecov:ignore -- requires Gmail/Drive API
	// Delete Drive file if known
	if g.store != nil {
		if rec, err := g.store.GetObject(ctx, bucket, key); err == nil && rec != nil && rec.DriveFileID != "" {
			_ = g.drive.Files.Delete(rec.DriveFileID).Context(ctx).Do()
		}
		_ = g.store.DeleteObject(ctx, bucket, key)
	}

	// Delete Gmail message
	_ = g.deleteByKey(ctx, bucket, key)

	// Clean up legacy chunks
	g.deleteChunked(ctx, bucket, key)
}

// fetchMetadataOnly finds the email for an object key and extracts metadata
// from the body text without downloading attachment data.
func (g *GmailBackend) fetchMetadataOnly(ctx context.Context, bucket, key string) (*objectMetadata, error) { // codecov:ignore -- requires Gmail API
	query := buildExactKeyQuery(g.labelPrefix, bucket, key)
	list, err := g.gmail.Users.Messages.List(g.user).Q(query).MaxResults(1).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("gmail search: %w", err)
	}
	if len(list.Messages) == 0 {
		return nil, ErrObjectNotFound
	}

	msg, err := g.gmail.Users.Messages.Get(g.user, list.Messages[0].Id).
		Format("full").
		Context(ctx).
		Do()
	if err != nil {
		return nil, fmt.Errorf("gmail get: %w", err)
	}

	// Extract body text from the message payload
	bodyText := extractBodyText(msg.Payload)
	if bodyText == "" {
		return nil, fmt.Errorf("no body text found in message %s", msg.Id)
	}

	return parseMetadataOnly(bodyText)
}

// extractBodyText pulls the plain text body from a Gmail message payload.
// Handles both simple (single-part) and multipart messages.
func extractBodyText(payload *gmail.MessagePart) string {
	if payload == nil {
		return ""
	}

	// Single-part message
	if payload.MimeType == "text/plain" && payload.Body != nil && payload.Body.Data != "" {
		data, err := base64.URLEncoding.DecodeString(payload.Body.Data)
		if err != nil {
			return ""
		}
		return string(data)
	}

	// Multipart message — find the text/plain part
	for _, part := range payload.Parts {
		if part.MimeType == "text/plain" && part.Body != nil && part.Body.Data != "" {
			data, err := base64.URLEncoding.DecodeString(part.Body.Data)
			if err != nil {
				continue
			}
			return string(data)
		}
	}

	return ""
}

// fetchObject finds and downloads the raw email for an object key, then
// parses the MIME message to extract metadata and attachment data.
func (g *GmailBackend) fetchObject(ctx context.Context, bucket, key string) (*objectMetadata, []byte, error) { // codecov:ignore -- requires Gmail API
	query := buildExactKeyQuery(g.labelPrefix, bucket, key)
	list, err := g.gmail.Users.Messages.List(g.user).Q(query).MaxResults(1).Context(ctx).Do()
	if err != nil {
		return nil, nil, fmt.Errorf("gmail search: %w", err)
	}
	if len(list.Messages) == 0 {
		return nil, nil, ErrObjectNotFound
	}

	msg, err := g.gmail.Users.Messages.Get(g.user, list.Messages[0].Id).
		Format("raw").
		Context(ctx).
		Do()
	if err != nil {
		return nil, nil, fmt.Errorf("gmail get: %w", err)
	}

	rawBytes, err := base64.URLEncoding.DecodeString(msg.Raw)
	if err != nil {
		return nil, nil, fmt.Errorf("decode raw message: %w", err)
	}

	return parseObjectEmail(rawBytes)
}

// deleteByKey finds and permanently deletes the email for a given object key.
// Returns nil if no matching email exists.
func (g *GmailBackend) deleteByKey(ctx context.Context, bucket, key string) error { // codecov:ignore -- requires Gmail API
	query := buildExactKeyQuery(g.labelPrefix, bucket, key)
	list, err := g.gmail.Users.Messages.List(g.user).Q(query).MaxResults(10).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("gmail search for delete: %w", err)
	}

	for _, msg := range list.Messages {
		if err := g.gmail.Users.Messages.Delete(g.user, msg.Id).Context(ctx).Do(); err != nil {
			slog.WarnContext(ctx, "Failed to delete gmail message",
				"message_id", msg.Id, "error", err,
			)
		}
	}
	return nil
}

// resolveLabelID looks up or creates the Gmail label for a bucket. Results
// are cached to avoid repeated API calls.
func (g *GmailBackend) resolveLabelID(ctx context.Context, bucket string) (string, error) { // codecov:ignore -- requires Gmail API
	name := labelName(g.labelPrefix, bucket)

	g.labelMu.RLock()
	id, ok := g.labelCache[name]
	g.labelMu.RUnlock()
	if ok {
		return id, nil
	}

	// List all labels and search for match
	labels, err := g.gmail.Users.Labels.List(g.user).Context(ctx).Do()
	if err != nil {
		return "", fmt.Errorf("list labels: %w", err)
	}
	for _, l := range labels.Labels {
		if l.Name == name {
			g.labelMu.Lock()
			g.labelCache[name] = l.Id
			g.labelMu.Unlock()
			return l.Id, nil
		}
	}

	return "", ErrBucketNotFound
}

// recordOp records Gmail API operation metrics.
func (g *GmailBackend) recordOp(operation string, start time.Time, err error) {
	status := "success"
	if err != nil {
		status = "error"
	}
	telemetry.GmailAPIRequestsTotal.WithLabelValues(operation, status).Inc()
	telemetry.GmailAPIDuration.WithLabelValues(operation).Observe(time.Since(start).Seconds())
}

// -------------------------------------------------------------------------------
// Gmail Backend - S3 Object Operations via Gmail and Google Drive
//
// Author: Alex Freidah
//
// Implements the ObjectBackend interface using Google Drive for object data
// and Gmail for metadata pointers. A local SQLite index eliminates API calls
// for metadata-only operations. All Google API calls are instrumented with
// OpenTelemetry client spans, Prometheus metrics, and structured logging
// with trace correlation.
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

	"go.opentelemetry.io/otel/codes"
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
// and refresh token, then initializes the Gmail and Drive API clients.
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
	query := fmt.Sprintf("name='%s' and mimeType='application/vnd.google-apps.folder' and trashed=false", name)
	list, err := svc.Files.List().Q(query).Fields("files(id)").Context(ctx).Do()
	if err != nil {
		return "", err
	}
	if len(list.Files) > 0 {
		return list.Files[0].Id, nil
	}

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
	ctx, span := telemetry.StartSpan(ctx, "Backend PutObject",
		telemetry.AttrBucket.String(bucket),
		telemetry.AttrObjectKey.String(key),
		telemetry.AttrObjectSize.Int64(size),
	)
	defer span.End()

	if contentType == "" {
		contentType = "application/octet-stream"
	}

	// Delete existing object if present (last-write-wins)
	g.deleteExisting(ctx, bucket, key)

	// Resolve label ID for bucket
	labelID, err := g.resolveLabelID(ctx, bucket)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		span.RecordError(err)
		g.recordBackendOp("PutObject", start, err)
		return "", fmt.Errorf("resolve label: %w", err)
	}

	// Stream body through MD5 hasher into Drive upload — avoids buffering
	// the entire object in memory.
	hasher := md5.New()
	tee := io.TeeReader(body, hasher)

	driveFileID, err := g.driveUpload(ctx, bucket+"/"+key, tee, size)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		span.RecordError(err)
		g.recordBackendOp("PutObject", start, err)
		return "", err
	}

	etag := fmt.Sprintf("%x", hasher.Sum(nil))

	// Insert metadata-only email in Gmail
	gmailMsgID, err := g.gmailInsertMetadata(ctx, bucket, key, labelID, &objectMetadata{
		ContentType: contentType,
		ETag:        etag,
		Size:        size,
		Metadata:    metadata,
		DriveFileID: driveFileID,
		CreatedAt:   time.Now().UTC(),
	})
	if err != nil {
		// Clean up Drive file on Gmail failure
		g.driveDelete(ctx, driveFileID)
		span.SetStatus(codes.Error, err.Error())
		span.RecordError(err)
		g.recordBackendOp("PutObject", start, err)
		return "", err
	}

	span.SetAttributes(
		telemetry.AttrGmailMsgID.String(gmailMsgID),
		telemetry.AttrDriveFileID.String(driveFileID),
	)

	// Record in metadata store
	if g.store != nil {
		_ = g.store.PutObject(ctx, &ObjectRecord{
			Bucket:      bucket,
			Key:         key,
			GmailMsgID:  gmailMsgID,
			DriveFileID: driveFileID,
			ETag:        etag,
			Size:        size,
			ContentType: contentType,
			CreatedAt:   time.Now().UTC(),
			Metadata:    metadata,
		})
	}

	telemetry.ObjectBytesUploaded.Add(float64(size))
	span.SetStatus(codes.Ok, "")
	g.recordBackendOp("PutObject", start, nil)

	slog.InfoContext(ctx, "Object stored",
		"bucket", bucket, "key", key, "size", size,
		"gmail_id", gmailMsgID, "drive_id", driveFileID,
	)

	return etag, nil
}

// GetObject retrieves object data from Google Drive using the file ID stored
// in the local index or Gmail email metadata.
func (g *GmailBackend) GetObject(ctx context.Context, bucket, key string) (*GetObjectResult, error) { // codecov:ignore -- requires Gmail/Drive API
	start := time.Now()
	ctx, span := telemetry.StartSpan(ctx, "Backend GetObject",
		telemetry.AttrBucket.String(bucket),
		telemetry.AttrObjectKey.String(key),
	)
	defer span.End()

	// Look up metadata from store or Gmail
	var driveFileID string
	var objSize int64
	var objContentType, objETag string
	var objCreatedAt time.Time
	var objMetadata map[string]string

	if g.store != nil {
		if rec, err := g.store.GetObject(ctx, bucket, key); err == nil && rec != nil {
			span.SetAttributes(telemetry.AttrCacheHit.Bool(true))
			driveFileID = rec.DriveFileID
			objSize = rec.Size
			objContentType = rec.ContentType
			objETag = rec.ETag
			objCreatedAt = rec.CreatedAt
			objMetadata = rec.Metadata
		}
	}

	if driveFileID == "" {
		span.SetAttributes(telemetry.AttrCacheHit.Bool(false))

		// Fallback: fetch metadata from Gmail
		meta, err := g.fetchMetadataOnly(ctx, bucket, key)
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
			span.RecordError(err)
			g.recordBackendOp("GetObject", start, err)
			return nil, err
		}
		driveFileID = meta.DriveFileID
		objSize = meta.Size
		objContentType = meta.ContentType
		objETag = meta.ETag
		objCreatedAt = meta.CreatedAt
		objMetadata = meta.Metadata

		if driveFileID == "" {
			// Legacy attachment-based object
			result, err := g.getLegacyObject(ctx, bucket, key, meta)
			g.recordBackendOp("GetObject", start, err)
			if err != nil {
				span.SetStatus(codes.Error, err.Error())
				span.RecordError(err)
			} else {
				span.SetStatus(codes.Ok, "")
			}
			return result, err
		}
	}

	// Download from Drive
	span.SetAttributes(telemetry.AttrDriveFileID.String(driveFileID))
	resp, err := g.driveDownload(ctx, driveFileID)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		span.RecordError(err)
		g.recordBackendOp("GetObject", start, err)
		return nil, err
	}

	telemetry.ObjectBytesDownloaded.Add(float64(objSize))
	span.SetStatus(codes.Ok, "")
	g.recordBackendOp("GetObject", start, nil)

	return &GetObjectResult{
		Body:         resp,
		Size:         objSize,
		ContentType:  objContentType,
		ETag:         objETag,
		LastModified: objCreatedAt,
		Metadata:     objMetadata,
	}, nil
}

// getLegacyObject handles reading objects stored as Gmail attachments
// (pre-Drive hybrid format).
func (g *GmailBackend) getLegacyObject(ctx context.Context, bucket, key string, meta *objectMetadata) (*GetObjectResult, error) { // codecov:ignore -- requires Gmail API
	ctx, span := telemetry.StartClientSpan(ctx, "Gmail.GetLegacyObject",
		telemetry.GmailAttributes("GetLegacyObject", bucket, key)...,
	)
	defer span.End()

	var data []byte
	var err error
	if meta.Chunked {
		data, err = g.getChunked(ctx, bucket, key, meta)
	} else {
		_, data, err = g.fetchObject(ctx, bucket, key)
	}
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		span.RecordError(err)
		return nil, err
	}

	span.SetStatus(codes.Ok, "")
	return &GetObjectResult{
		Body:         io.NopCloser(bytes.NewReader(data)),
		Size:         meta.Size,
		ContentType:  meta.ContentType,
		ETag:         meta.ETag,
		LastModified: meta.CreatedAt,
		Metadata:     meta.Metadata,
	}, nil
}

// HeadObject retrieves only the metadata for an object. Checks the local
// SQLite index first (zero API calls). Falls back to Gmail API on cache miss.
func (g *GmailBackend) HeadObject(ctx context.Context, bucket, key string) (*HeadObjectResult, error) { // codecov:ignore -- requires Gmail API
	start := time.Now()
	ctx, span := telemetry.StartSpan(ctx, "Backend HeadObject",
		telemetry.AttrBucket.String(bucket),
		telemetry.AttrObjectKey.String(key),
	)
	defer span.End()

	// Check local store first
	if g.store != nil {
		rec, err := g.store.GetObject(ctx, bucket, key)
		if err == nil && rec != nil {
			span.SetAttributes(telemetry.AttrCacheHit.Bool(true))
			span.SetStatus(codes.Ok, "")
			g.recordBackendOp("HeadObject", start, nil)
			return &HeadObjectResult{
				Size:         rec.Size,
				ContentType:  rec.ContentType,
				ETag:         rec.ETag,
				LastModified: rec.CreatedAt,
				Metadata:     rec.Metadata,
			}, nil
		}
	}

	span.SetAttributes(telemetry.AttrCacheHit.Bool(false))

	// Fallback to Gmail API
	meta, err := g.fetchMetadataOnly(ctx, bucket, key)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		span.RecordError(err)
		g.recordBackendOp("HeadObject", start, err)
		return nil, err
	}

	span.SetStatus(codes.Ok, "")
	g.recordBackendOp("HeadObject", start, nil)
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
	ctx, span := telemetry.StartSpan(ctx, "Backend DeleteObject",
		telemetry.AttrBucket.String(bucket),
		telemetry.AttrObjectKey.String(key),
	)
	defer span.End()

	g.deleteExisting(ctx, bucket, key)

	span.SetStatus(codes.Ok, "")
	g.recordBackendOp("DeleteObject", start, nil)
	return nil
}

// -------------------------------------------------------------------------
// DRIVE API HELPERS (instrumented)
// -------------------------------------------------------------------------

// driveUpload streams data to Google Drive and returns the file ID.
func (g *GmailBackend) driveUpload(ctx context.Context, name string, body io.Reader, size int64) (string, error) { // codecov:ignore -- requires Drive API
	start := time.Now()
	ctx, span := telemetry.StartClientSpan(ctx, "Drive.Files.Create",
		telemetry.AttrOperation.String("upload"),
		telemetry.AttrObjectSize.Int64(size),
	)
	defer span.End()

	file, err := g.drive.Files.Create(&drive.File{
		Name:    name,
		Parents: []string{g.driveFolderID},
	}).Media(body).Fields("id").Context(ctx).Do()

	g.recordDriveOp("upload", start, err)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		span.RecordError(err)
		return "", fmt.Errorf("drive upload: %w", err)
	}

	span.SetAttributes(telemetry.AttrDriveFileID.String(file.Id))
	span.SetStatus(codes.Ok, "")
	return file.Id, nil
}

// driveDownload downloads a file from Google Drive by ID.
func (g *GmailBackend) driveDownload(ctx context.Context, fileID string) (io.ReadCloser, error) { // codecov:ignore -- requires Drive API
	start := time.Now()
	_, span := telemetry.StartClientSpan(ctx, "Drive.Files.Get",
		telemetry.AttrOperation.String("download"),
		telemetry.AttrDriveFileID.String(fileID),
	)
	defer span.End()

	resp, err := g.drive.Files.Get(fileID).Download()
	g.recordDriveOp("download", start, err)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		span.RecordError(err)
		return nil, fmt.Errorf("drive download: %w", err)
	}

	span.SetStatus(codes.Ok, "")
	return resp.Body, nil
}

// driveDelete deletes a file from Google Drive by ID.
func (g *GmailBackend) driveDelete(ctx context.Context, fileID string) { // codecov:ignore -- requires Drive API
	if fileID == "" {
		return
	}
	start := time.Now()
	ctx, span := telemetry.StartClientSpan(ctx, "Drive.Files.Delete",
		telemetry.AttrOperation.String("delete"),
		telemetry.AttrDriveFileID.String(fileID),
	)
	defer span.End()

	err := g.drive.Files.Delete(fileID).Context(ctx).Do()
	g.recordDriveOp("delete", start, err)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		span.RecordError(err)
		slog.WarnContext(ctx, "Failed to delete drive file", "file_id", fileID, "error", err)
	} else {
		span.SetStatus(codes.Ok, "")
	}
}

// -------------------------------------------------------------------------
// GMAIL API HELPERS (instrumented)
// -------------------------------------------------------------------------

// gmailInsertMetadata inserts a metadata-only email in Gmail.
func (g *GmailBackend) gmailInsertMetadata(ctx context.Context, bucket, key, labelID string, meta *objectMetadata) (string, error) { // codecov:ignore -- requires Gmail API
	start := time.Now()
	ctx, span := telemetry.StartClientSpan(ctx, "Gmail.Messages.Insert",
		telemetry.GmailAttributes("Insert", bucket, key)...,
	)
	defer span.End()

	subject := objectSubject(bucket, key)
	rawEmail, err := buildObjectEmail(subject, meta, nil)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		span.RecordError(err)
		g.recordGmailOp("Insert", start, err)
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

	g.recordGmailOp("Insert", start, err)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		span.RecordError(err)
		return "", fmt.Errorf("gmail insert: %w", err)
	}

	span.SetAttributes(telemetry.AttrGmailMsgID.String(sent.Id))
	span.SetStatus(codes.Ok, "")
	return sent.Id, nil
}

// deleteExisting removes all traces of an object -- Drive file, Gmail
// message, legacy chunks, and metadata store record.
func (g *GmailBackend) deleteExisting(ctx context.Context, bucket, key string) { // codecov:ignore -- requires Gmail/Drive API
	ctx, span := telemetry.StartSpan(ctx, "Backend.deleteExisting",
		telemetry.AttrBucket.String(bucket),
		telemetry.AttrObjectKey.String(key),
	)
	defer span.End()

	// Delete Drive file if known
	if g.store != nil {
		if rec, err := g.store.GetObject(ctx, bucket, key); err == nil && rec != nil && rec.DriveFileID != "" {
			g.driveDelete(ctx, rec.DriveFileID)
		}
		_ = g.store.DeleteObject(ctx, bucket, key)
	}

	// Delete Gmail message
	_ = g.deleteByKey(ctx, bucket, key)

	// Clean up legacy chunks
	g.deleteChunked(ctx, bucket, key)

	span.SetStatus(codes.Ok, "")
}

// fetchMetadataOnly finds the email for an object key and extracts metadata
// from the body text without downloading attachment data.
func (g *GmailBackend) fetchMetadataOnly(ctx context.Context, bucket, key string) (*objectMetadata, error) { // codecov:ignore -- requires Gmail API
	start := time.Now()
	ctx, span := telemetry.StartClientSpan(ctx, "Gmail.Messages.Get",
		telemetry.GmailAttributes("GetMetadata", bucket, key)...,
	)
	defer span.End()

	query := buildExactKeyQuery(g.labelPrefix, bucket, key)
	list, err := g.gmail.Users.Messages.List(g.user).Q(query).MaxResults(1).Context(ctx).Do()
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		span.RecordError(err)
		g.recordGmailOp("List", start, err)
		return nil, fmt.Errorf("gmail search: %w", err)
	}
	g.recordGmailOp("List", start, nil)

	if len(list.Messages) == 0 {
		span.SetStatus(codes.Error, "not found")
		return nil, ErrObjectNotFound
	}

	msgStart := time.Now()
	msg, err := g.gmail.Users.Messages.Get(g.user, list.Messages[0].Id).
		Format("full").
		Context(ctx).
		Do()
	g.recordGmailOp("Get", msgStart, err)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		span.RecordError(err)
		return nil, fmt.Errorf("gmail get: %w", err)
	}

	bodyText := extractBodyText(msg.Payload)
	if bodyText == "" {
		span.SetStatus(codes.Error, "no body text")
		return nil, fmt.Errorf("no body text found in message %s", msg.Id)
	}

	span.SetAttributes(telemetry.AttrGmailMsgID.String(msg.Id))
	span.SetStatus(codes.Ok, "")
	return parseMetadataOnly(bodyText)
}

// extractBodyText pulls the plain text body from a Gmail message payload.
// Handles both simple (single-part) and multipart messages.
func extractBodyText(payload *gmail.MessagePart) string {
	if payload == nil {
		return ""
	}

	if payload.MimeType == "text/plain" && payload.Body != nil && payload.Body.Data != "" {
		data, err := base64.URLEncoding.DecodeString(payload.Body.Data)
		if err != nil {
			return ""
		}
		return string(data)
	}

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
	start := time.Now()
	ctx, span := telemetry.StartClientSpan(ctx, "Gmail.Messages.GetRaw",
		telemetry.GmailAttributes("GetRaw", bucket, key)...,
	)
	defer span.End()

	query := buildExactKeyQuery(g.labelPrefix, bucket, key)
	list, err := g.gmail.Users.Messages.List(g.user).Q(query).MaxResults(1).Context(ctx).Do()
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		span.RecordError(err)
		g.recordGmailOp("List", start, err)
		return nil, nil, fmt.Errorf("gmail search: %w", err)
	}
	g.recordGmailOp("List", start, nil)

	if len(list.Messages) == 0 {
		span.SetStatus(codes.Error, "not found")
		return nil, nil, ErrObjectNotFound
	}

	msgStart := time.Now()
	msg, err := g.gmail.Users.Messages.Get(g.user, list.Messages[0].Id).
		Format("raw").
		Context(ctx).
		Do()
	g.recordGmailOp("GetRaw", msgStart, err)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		span.RecordError(err)
		return nil, nil, fmt.Errorf("gmail get: %w", err)
	}

	rawBytes, err := base64.URLEncoding.DecodeString(msg.Raw)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		span.RecordError(err)
		return nil, nil, fmt.Errorf("decode raw message: %w", err)
	}

	span.SetStatus(codes.Ok, "")
	return parseObjectEmail(rawBytes)
}

// deleteByKey finds and trashes all emails for a given object key.
func (g *GmailBackend) deleteByKey(ctx context.Context, bucket, key string) error { // codecov:ignore -- requires Gmail API
	start := time.Now()
	ctx, span := telemetry.StartClientSpan(ctx, "Gmail.Messages.Trash",
		telemetry.GmailAttributes("Trash", bucket, key)...,
	)
	defer span.End()

	query := buildExactKeyQuery(g.labelPrefix, bucket, key)
	list, err := g.gmail.Users.Messages.List(g.user).Q(query).MaxResults(10).Context(ctx).Do()
	g.recordGmailOp("List", start, err)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		span.RecordError(err)
		return fmt.Errorf("gmail search for delete: %w", err)
	}

	for _, msg := range list.Messages {
		trashStart := time.Now()
		if _, trashErr := g.gmail.Users.Messages.Trash(g.user, msg.Id).Context(ctx).Do(); trashErr != nil {
			g.recordGmailOp("Trash", trashStart, trashErr)
			slog.WarnContext(ctx, "Failed to trash gmail message",
				"message_id", msg.Id, "error", trashErr,
			)
		} else {
			g.recordGmailOp("Trash", trashStart, nil)
		}
	}

	span.SetStatus(codes.Ok, "")
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
		telemetry.LabelCacheHitsTotal.Inc()
		return id, nil
	}

	telemetry.LabelCacheMissesTotal.Inc()

	start := time.Now()
	labels, err := g.gmail.Users.Labels.List(g.user).Context(ctx).Do()
	g.recordGmailOp("ListLabels", start, err)
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

// -------------------------------------------------------------------------
// METRICS HELPERS
// -------------------------------------------------------------------------

// recordBackendOp records a backend-level operation metric (PutObject, GetObject, etc.).
func (g *GmailBackend) recordBackendOp(operation string, start time.Time, err error) {
	status := "success"
	if err != nil {
		status = "error"
	}
	telemetry.BackendRequestsTotal.WithLabelValues(operation, status).Inc()
	telemetry.BackendDuration.WithLabelValues(operation).Observe(time.Since(start).Seconds())
}

// recordGmailOp records a Gmail API operation metric.
func (g *GmailBackend) recordGmailOp(operation string, start time.Time, err error) {
	status := "success"
	if err != nil {
		status = "error"
	}
	telemetry.GmailAPIRequestsTotal.WithLabelValues(operation, status).Inc()
	telemetry.GmailAPIDuration.WithLabelValues(operation).Observe(time.Since(start).Seconds())
}

// recordDriveOp records a Drive API operation metric.
func (g *GmailBackend) recordDriveOp(operation string, start time.Time, err error) {
	status := "success"
	if err != nil {
		status = "error"
	}
	telemetry.DriveAPIRequestsTotal.WithLabelValues(operation, status).Inc()
	telemetry.DriveAPIDuration.WithLabelValues(operation).Observe(time.Since(start).Seconds())
}

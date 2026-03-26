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
	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"
)

// -------------------------------------------------------------------------
// GMAIL BACKEND
// -------------------------------------------------------------------------

// GmailBackend implements ObjectBackend using the Gmail API.
type GmailBackend struct {
	svc         *gmail.Service
	user        string
	labelPrefix string
	maxAttach   int64
	chunkSize   int64

	// labelCache maps bucket name to Gmail label ID
	labelMu    sync.RWMutex
	labelCache map[string]string
}

// NewGmailBackend creates a GmailBackend from the provided configuration.
// It builds an OAuth2 token source from the configured client credentials
// and refresh token, then initializes the Gmail API client.
func NewGmailBackend(ctx context.Context, cfg *config.GmailConfig) (*GmailBackend, error) {
	oauthCfg := &oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		Endpoint:     google.Endpoint,
		Scopes:       []string{gmail.GmailModifyScope},
	}

	tok := &oauth2.Token{RefreshToken: cfg.RefreshToken}
	client := oauthCfg.Client(ctx, tok)

	svc, err := gmail.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		return nil, fmt.Errorf("create gmail service: %w", err)
	}

	return &GmailBackend{
		svc:         svc,
		user:        cfg.User,
		labelPrefix: cfg.LabelPrefix,
		maxAttach:   cfg.MaxAttachmentBytes,
		chunkSize:   cfg.ChunkSizeBytes,
		labelCache:  make(map[string]string),
	}, nil
}

// -------------------------------------------------------------------------
// OBJECT OPERATIONS
// -------------------------------------------------------------------------

// PutObject stores an object as a Gmail email with metadata body and data
// attachment. If an object with the same key already exists, it is deleted
// first (last-write-wins semantics).
func (g *GmailBackend) PutObject(ctx context.Context, bucket, key string, body io.Reader, size int64, contentType string, metadata map[string]string) (string, error) {
	start := time.Now()
	ctx, span := telemetry.StartClientSpan(ctx, "Gmail.SendMessage",
		telemetry.GmailAttributes("PutObject", bucket, key)...,
	)
	defer span.End()

	// Read entire object into memory for email construction
	data, err := io.ReadAll(body)
	if err != nil {
		g.recordOp("PutObject", start, err)
		return "", fmt.Errorf("read body: %w", err)
	}

	// Delete existing object if present (last-write-wins)
	_ = g.deleteByKey(ctx, bucket, key)
	g.deleteChunked(ctx, bucket, key)

	// Resolve label ID for bucket
	labelID, err := g.resolveLabelID(ctx, bucket)
	if err != nil {
		g.recordOp("PutObject", start, err)
		return "", fmt.Errorf("resolve label: %w", err)
	}

	if contentType == "" {
		contentType = "application/octet-stream"
	}

	// Route to chunked write if object exceeds chunk size
	if int64(len(data)) > g.chunkSize {
		etag, err := g.putChunked(ctx, bucket, key, data, contentType, metadata, labelID)
		g.recordOp("PutObject", start, err)
		return etag, err
	}

	// Single-email write path
	hash := md5.Sum(data)
	etag := fmt.Sprintf("%x", hash)

	meta := &objectMetadata{
		ContentType: contentType,
		ETag:        etag,
		Size:        int64(len(data)),
		Metadata:    metadata,
		CreatedAt:   time.Now().UTC(),
	}
	subject := objectSubject(bucket, key)
	rawEmail, err := buildObjectEmail(subject, meta, data)
	if err != nil {
		g.recordOp("PutObject", start, err)
		return "", fmt.Errorf("build email: %w", err)
	}

	msg := &gmail.Message{
		Raw:      base64.URLEncoding.EncodeToString(rawEmail),
		LabelIds: []string{labelID},
	}
	sent, err := g.svc.Users.Messages.Insert(g.user, msg).
		InternalDateSource("dateHeader").
		Context(ctx).
		Do()
	if err != nil {
		g.recordOp("PutObject", start, err)
		return "", fmt.Errorf("gmail insert: %w", err)
	}

	span.SetAttributes(telemetry.AttrGmailMsgID.String(sent.Id))
	slog.InfoContext(ctx, "Object stored",
		"bucket", bucket, "key", key, "size", len(data), "gmail_id", sent.Id,
	)

	g.recordOp("PutObject", start, nil)
	return etag, nil
}

// GetObject retrieves an object from Gmail by searching for the email with
// the matching subject line, then parsing the MIME message to extract
// metadata and attachment data.
func (g *GmailBackend) GetObject(ctx context.Context, bucket, key string) (*GetObjectResult, error) {
	start := time.Now()
	ctx, span := telemetry.StartClientSpan(ctx, "Gmail.GetMessage",
		telemetry.GmailAttributes("GetObject", bucket, key)...,
	)
	defer span.End()

	meta, data, err := g.fetchObject(ctx, bucket, key)
	if err != nil {
		g.recordOp("GetObject", start, err)
		return nil, err
	}

	// Reassemble chunked objects
	if meta.Chunked {
		data, err = g.getChunked(ctx, bucket, key, meta)
		if err != nil {
			g.recordOp("GetObject", start, err)
			return nil, fmt.Errorf("reassemble chunks: %w", err)
		}
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

// HeadObject retrieves only the metadata for an object without downloading
// the attachment data.
func (g *GmailBackend) HeadObject(ctx context.Context, bucket, key string) (*HeadObjectResult, error) {
	start := time.Now()
	ctx, span := telemetry.StartClientSpan(ctx, "Gmail.GetMessage",
		telemetry.GmailAttributes("HeadObject", bucket, key)...,
	)
	defer span.End()

	meta, _, err := g.fetchObject(ctx, bucket, key)
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

// DeleteObject removes an object by finding and trashing the corresponding
// Gmail message. Returns nil if the object does not exist (S3 idempotency).
func (g *GmailBackend) DeleteObject(ctx context.Context, bucket, key string) error {
	start := time.Now()
	ctx, span := telemetry.StartClientSpan(ctx, "Gmail.DeleteMessage",
		telemetry.GmailAttributes("DeleteObject", bucket, key)...,
	)
	defer span.End()

	err := g.deleteByKey(ctx, bucket, key)
	g.deleteChunked(ctx, bucket, key)
	g.recordOp("DeleteObject", start, err)
	return err
}

// -------------------------------------------------------------------------
// INTERNAL HELPERS
// -------------------------------------------------------------------------

// fetchObject finds and downloads the raw email for an object key, then
// parses the MIME message to extract metadata and attachment data.
func (g *GmailBackend) fetchObject(ctx context.Context, bucket, key string) (*objectMetadata, []byte, error) {
	query := buildExactKeyQuery(g.labelPrefix, bucket, key)
	list, err := g.svc.Users.Messages.List(g.user).Q(query).MaxResults(1).Context(ctx).Do()
	if err != nil {
		return nil, nil, fmt.Errorf("gmail search: %w", err)
	}
	if len(list.Messages) == 0 {
		return nil, nil, ErrObjectNotFound
	}

	msg, err := g.svc.Users.Messages.Get(g.user, list.Messages[0].Id).
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
func (g *GmailBackend) deleteByKey(ctx context.Context, bucket, key string) error {
	query := buildExactKeyQuery(g.labelPrefix, bucket, key)
	list, err := g.svc.Users.Messages.List(g.user).Q(query).MaxResults(10).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("gmail search for delete: %w", err)
	}

	for _, msg := range list.Messages {
		if err := g.svc.Users.Messages.Delete(g.user, msg.Id).Context(ctx).Do(); err != nil {
			slog.WarnContext(ctx, "Failed to delete gmail message",
				"message_id", msg.Id, "error", err,
			)
		}
	}
	return nil
}

// resolveLabelID looks up or creates the Gmail label for a bucket. Results
// are cached to avoid repeated API calls.
func (g *GmailBackend) resolveLabelID(ctx context.Context, bucket string) (string, error) {
	name := labelName(g.labelPrefix, bucket)

	g.labelMu.RLock()
	id, ok := g.labelCache[name]
	g.labelMu.RUnlock()
	if ok {
		return id, nil
	}

	// List all labels and search for match
	labels, err := g.svc.Users.Labels.List(g.user).Context(ctx).Do()
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

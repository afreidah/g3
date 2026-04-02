// -------------------------------------------------------------------------------
// Gmail Chunked Operations - Large Object Support
//
// Author: Alex Freidah
//
// Handles objects that exceed Gmail's 25MB attachment limit by splitting them
// across multiple emails. Each chunk is stored as a separate email with a
// numbered subject suffix. A manifest email with the original object key
// stores metadata including chunk count, total size, and ETag. Read and
// delete operations reassemble or clean up all chunks transparently.
// -------------------------------------------------------------------------------

package backend

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/base64"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/afreidah/g3/internal/telemetry"

	"go.opentelemetry.io/otel/attribute"
	"google.golang.org/api/gmail/v1"
)

// -------------------------------------------------------------------------
// CHUNKED WRITE
// -------------------------------------------------------------------------

// putChunked splits a large object into chunks and stores each as a separate
// email. A manifest email records the chunk count and overall metadata.
// Returns the ETag computed across all chunks.
func (g *GmailBackend) putChunked(ctx context.Context, bucket, key string, data []byte, contentType string, metadata map[string]string, labelID string) (string, error) {
	ctx, span := telemetry.StartSpan(ctx, "Gmail.ChunkWrite",
		telemetry.GmailAttributes("PutChunked", bucket, key)...,
	)
	defer span.End()

	// Compute ETag over entire object
	hash := md5.Sum(data)
	etag := fmt.Sprintf("%x", hash)

	chunkCount := 0
	offset := 0

	// Send chunk emails
	for offset < len(data) {
		end := offset + int(g.chunkSize)
		if end > len(data) {
			end = len(data)
		}
		chunk := data[offset:end]
		chunkCount++

		subject := chunkSubject(bucket, key, chunkCount)
		chunkMeta := &objectMetadata{
			ContentType: "application/octet-stream",
			Size:        int64(len(chunk)),
			CreatedAt:   time.Now().UTC(),
		}

		rawEmail, err := buildObjectEmail(subject, chunkMeta, chunk)
		if err != nil {
			return "", fmt.Errorf("build chunk %d email: %w", chunkCount, err)
		}

		msg := &gmail.Message{
			Raw:      base64.URLEncoding.EncodeToString(rawEmail),
			LabelIds: []string{labelID},
		}
		_, err = g.gmail.Users.Messages.Insert(g.user, msg).
			InternalDateSource("dateHeader").
			Context(ctx).
			Do()
		if err != nil {
			return "", fmt.Errorf("gmail insert chunk %d: %w", chunkCount, err)
		}

		offset = end
	}

	span.SetAttributes(
		telemetry.AttrChunkCount.Int(chunkCount),
		attribute.Int64("g3.object.total_size", int64(len(data))),
	)

	// Send manifest email (no attachment, just metadata)
	manifest := &objectMetadata{
		ContentType: contentType,
		ETag:        etag,
		Size:        int64(len(data)),
		Metadata:    metadata,
		Chunked:     true,
		ChunkCount:  chunkCount,
		TotalSize:   int64(len(data)),
		CreatedAt:   time.Now().UTC(),
	}

	subject := objectSubject(bucket, key)
	rawEmail, err := buildObjectEmail(subject, manifest, nil)
	if err != nil {
		return "", fmt.Errorf("build manifest email: %w", err)
	}

	msg := &gmail.Message{
		Raw:      base64.URLEncoding.EncodeToString(rawEmail),
		LabelIds: []string{labelID},
	}
	_, err = g.gmail.Users.Messages.Insert(g.user, msg).
		InternalDateSource("dateHeader").
		Context(ctx).
		Do()
	if err != nil {
		return "", fmt.Errorf("gmail insert manifest: %w", err)
	}

	slog.InfoContext(ctx, "Chunked object stored",
		"bucket", bucket, "key", key, "chunks", chunkCount, "total_size", len(data),
	)

	return etag, nil
}

// -------------------------------------------------------------------------
// CHUNKED READ
// -------------------------------------------------------------------------

// getChunked retrieves all chunks for a chunked object and reassembles them
// into a single byte slice. Returns the manifest metadata and assembled data.
func (g *GmailBackend) getChunked(ctx context.Context, bucket, key string, meta *objectMetadata) ([]byte, error) {
	ctx, span := telemetry.StartSpan(ctx, "Gmail.ChunkAssemble",
		telemetry.GmailAttributes("GetChunked", bucket, key)...,
	)
	defer span.End()

	span.SetAttributes(telemetry.AttrChunkCount.Int(meta.ChunkCount))

	query := buildChunkQuery(g.labelPrefix, bucket, key)
	list, err := g.gmail.Users.Messages.List(g.user).
		Q(query).
		MaxResults(int64(meta.ChunkCount + 10)).
		Context(ctx).
		Do()
	if err != nil {
		return nil, fmt.Errorf("gmail search chunks: %w", err)
	}

	if len(list.Messages) == 0 {
		return nil, fmt.Errorf("no chunks found for %s/%s", bucket, key)
	}

	// Fetch and parse each chunk
	type chunkData struct {
		index int
		data  []byte
	}
	chunks := make([]chunkData, 0, len(list.Messages))

	for _, msgRef := range list.Messages {
		msg, err := g.gmail.Users.Messages.Get(g.user, msgRef.Id).
			Format("raw").
			Context(ctx).
			Do()
		if err != nil {
			return nil, fmt.Errorf("gmail get chunk: %w", err)
		}

		rawBytes, err := base64.URLEncoding.DecodeString(msg.Raw)
		if err != nil {
			return nil, fmt.Errorf("decode chunk message: %w", err)
		}

		_, attachData, err := parseObjectEmail(rawBytes)
		if err != nil {
			return nil, fmt.Errorf("parse chunk email: %w", err)
		}

		// Extract chunk index from subject
		subject := ""
		for _, h := range msg.Payload.Headers {
			if h.Name == "Subject" {
				subject = h.Value
				break
			}
		}
		// Subject is fetched from raw, so parse from the raw email headers
		// Since we used Format("raw"), Payload.Headers may be empty; parse from raw
		idx := parseChunkIndex(subject)
		if idx <= 0 {
			// Try parsing from the raw email
			idx = parseChunkIndexFromRaw(rawBytes, bucket, key)
		}

		chunks = append(chunks, chunkData{index: idx, data: attachData})
	}

	// Sort by chunk index
	sort.Slice(chunks, func(i, j int) bool {
		return chunks[i].index < chunks[j].index
	})

	// Reassemble
	var assembled bytes.Buffer
	assembled.Grow(int(meta.TotalSize))
	for _, c := range chunks {
		assembled.Write(c.data)
	}

	return assembled.Bytes(), nil
}

// -------------------------------------------------------------------------
// CHUNKED DELETE
// -------------------------------------------------------------------------

// deleteChunked removes all chunk emails for a chunked object.
func (g *GmailBackend) deleteChunked(ctx context.Context, bucket, key string) {
	query := buildChunkQuery(g.labelPrefix, bucket, key)
	list, err := g.gmail.Users.Messages.List(g.user).
		Q(query).
		MaxResults(1000).
		Context(ctx).
		Do()
	if err != nil {
		slog.WarnContext(ctx, "Failed to list chunks for deletion",
			"bucket", bucket, "key", key, "error", err,
		)
		return
	}

	for _, msg := range list.Messages {
		if err := g.gmail.Users.Messages.Delete(g.user, msg.Id).Context(ctx).Do(); err != nil {
			slog.WarnContext(ctx, "Failed to delete chunk",
				"message_id", msg.Id, "error", err,
			)
		}
	}
}

// -------------------------------------------------------------------------
// HELPERS
// -------------------------------------------------------------------------

// parseChunkIndex extracts the chunk number from a subject line like
// "s3://bucket/key#chunk-003". Returns 0 if parsing fails.
func parseChunkIndex(subject string) int {
	idx := bytes.LastIndex([]byte(subject), []byte("#chunk-"))
	if idx < 0 {
		return 0
	}
	numStr := subject[idx+7:]
	var n int
	_, _ = fmt.Sscanf(numStr, "%d", &n)
	return n
}

// parseChunkIndexFromRaw extracts the chunk index by scanning the raw email
// for the Subject header. Used when Format("raw") doesn't populate
// Payload.Headers.
func parseChunkIndexFromRaw(raw []byte, bucket, key string) int {
	lines := bytes.Split(raw, []byte("\n"))
	for _, line := range lines {
		trimmed := bytes.TrimSpace(line)
		if bytes.HasPrefix(trimmed, []byte("Subject: ")) {
			subject := string(bytes.TrimPrefix(trimmed, []byte("Subject: ")))
			return parseChunkIndex(subject)
		}
	}
	return 0
}

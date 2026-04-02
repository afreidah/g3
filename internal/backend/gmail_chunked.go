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
	"encoding/base64"
	"fmt"
	"log/slog"
	"sort"

	"github.com/afreidah/g3/internal/telemetry"
)

// -------------------------------------------------------------------------
// CHUNKED READ
// -------------------------------------------------------------------------

// getChunked retrieves all chunks for a chunked object and reassembles them
// into a single byte slice. Returns the manifest metadata and assembled data.
func (g *GmailBackend) getChunked(ctx context.Context, bucket, key string, meta *objectMetadata) ([]byte, error) { // codecov:ignore -- requires Gmail API
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
func (g *GmailBackend) deleteChunked(ctx context.Context, bucket, key string) { // codecov:ignore -- requires Gmail API
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

// -------------------------------------------------------------------------------
// Gmail List Operations - ListObjects, ListBuckets, CreateBucket
//
// Author: Alex Freidah
//
// Implements bucket and object listing operations by querying Gmail labels and
// searching for emails by subject prefix. CreateBucket creates a new Gmail
// label under the configured prefix.
// -------------------------------------------------------------------------------

package backend

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/afreidah/g3/internal/telemetry"

	"google.golang.org/api/gmail/v1"
)

// -------------------------------------------------------------------------
// BUCKET OPERATIONS
// -------------------------------------------------------------------------

// ListBuckets returns all Gmail labels that match the configured label prefix,
// interpreted as S3 buckets.
func (g *GmailBackend) ListBuckets(ctx context.Context) ([]BucketInfo, error) {
	start := time.Now()
	ctx, span := telemetry.StartClientSpan(ctx, "Gmail.ListLabels",
		telemetry.GmailAttributes("ListBuckets", "", "")...,
	)
	defer span.End()

	labels, err := g.gmail.Users.Labels.List(g.user).Context(ctx).Do()
	if err != nil {
		g.recordGmailOp("ListBuckets", start, err)
		return nil, fmt.Errorf("list labels: %w", err)
	}

	prefix := g.labelPrefix + "/"
	var buckets []BucketInfo
	for _, l := range labels.Labels {
		if strings.HasPrefix(l.Name, prefix) {
			name := strings.TrimPrefix(l.Name, prefix)
			if name != "" && !strings.Contains(name, "/") {
				buckets = append(buckets, BucketInfo{
					Name:         name,
					CreationDate: time.Time{},
				})
			}
		}
	}

	// Cache discovered labels
	g.labelMu.Lock()
	for _, l := range labels.Labels {
		if strings.HasPrefix(l.Name, prefix) {
			g.labelCache[l.Name] = l.Id
		}
	}
	g.labelMu.Unlock()

	g.recordGmailOp("ListBuckets", start, nil)
	return buckets, nil
}

// CreateBucket creates a new Gmail label for the given bucket name.
func (g *GmailBackend) CreateBucket(ctx context.Context, bucket string) error {
	start := time.Now()
	ctx, span := telemetry.StartClientSpan(ctx, "Gmail.CreateLabel",
		telemetry.GmailAttributes("CreateBucket", bucket, "")...,
	)
	defer span.End()

	name := labelName(g.labelPrefix, bucket)

	// Check if label already exists
	labels, err := g.gmail.Users.Labels.List(g.user).Context(ctx).Do()
	if err != nil {
		g.recordGmailOp("CreateBucket", start, err)
		return fmt.Errorf("list labels: %w", err)
	}
	for _, l := range labels.Labels {
		if l.Name == name {
			g.recordGmailOp("CreateBucket", start, nil)
			return ErrBucketExists
		}
	}

	label := &gmail.Label{
		Name:                  name,
		LabelListVisibility:   "labelHide",
		MessageListVisibility: "hide",
	}
	created, err := g.gmail.Users.Labels.Create(g.user, label).Context(ctx).Do()
	if err != nil {
		g.recordGmailOp("CreateBucket", start, err)
		return fmt.Errorf("create label: %w", err)
	}

	g.labelMu.Lock()
	g.labelCache[name] = created.Id
	g.labelMu.Unlock()

	slog.InfoContext(ctx, "Bucket created", "bucket", bucket, "label_id", created.Id)
	g.recordGmailOp("CreateBucket", start, nil)
	return nil
}

// -------------------------------------------------------------------------
// OBJECT LISTING
// -------------------------------------------------------------------------

// ListObjects returns objects matching the given bucket and prefix in S3
// ListObjectsV2 format with delimiter and pagination support. When the local
// metadata index is available it serves the listing from a single indexed
// query; the Gmail crawl is used only as a fallback when the index is absent or
// unavailable. The Gmail path is an N+1 fetch (one message Get per object) and
// cannot complete within client timeouts on large buckets, so the index is
// strongly preferred.
func (g *GmailBackend) ListObjects(ctx context.Context, bucket, prefix, delimiter, startAfter string, maxKeys int) (*ListObjectsResult, error) {
	ctx, span := telemetry.StartClientSpan(ctx, "Gmail.ListMessages",
		telemetry.GmailAttributes("ListObjects", bucket, prefix)...,
	)
	defer span.End()

	if maxKeys <= 0 {
		maxKeys = 1000
	}

	// Serve from the metadata index when present. On index error (not an empty
	// result) fall back to Gmail so a transient DB outage does not break
	// listing entirely.
	if g.store != nil {
		result, err := g.listFromStore(ctx, bucket, prefix, delimiter, startAfter, maxKeys)
		if err == nil {
			return result, nil
		}
		slog.WarnContext(ctx, "Metadata index list failed, falling back to Gmail",
			"bucket", bucket, "error", err,
		)
	}

	return g.listFromGmail(ctx, bucket, prefix, delimiter, startAfter, maxKeys)
}

// listFromStore builds a ListObjectsV2 response from the local metadata index.
// The index holds key, size, and ETag for every object, so the listing is a
// single key-ordered query rather than a per-object Gmail fetch.
func (g *GmailBackend) listFromStore(ctx context.Context, bucket, prefix, delimiter, startAfter string, maxKeys int) (*ListObjectsResult, error) {
	records, err := g.store.ListObjects(ctx, bucket, prefix, startAfter, maxKeys)
	if err != nil {
		return nil, err
	}

	objects := make([]ObjectInfo, 0, len(records))
	for _, rec := range records {
		// Skip chunk component emails that should never surface as objects.
		if strings.Contains(rec.Key, "#chunk-") {
			continue
		}
		objects = append(objects, ObjectInfo{
			Key:          rec.Key,
			Size:         rec.Size,
			ETag:         rec.ETag,
			LastModified: rec.CreatedAt,
		})
	}

	return collapseListResult(objects, prefix, delimiter, maxKeys), nil
}

// listFromGmail builds a ListObjectsV2 response by searching Gmail and fetching
// each matching message for its metadata. This is an N+1 fetch retained only
// for the store-absent fallback case.
func (g *GmailBackend) listFromGmail(ctx context.Context, bucket, prefix, delimiter, startAfter string, maxKeys int) (*ListObjectsResult, error) {
	start := time.Now()

	query := buildListQuery(g.labelPrefix, bucket, prefix)
	allMessages, err := g.listGmailMessageIDs(ctx, query, maxKeys, start)
	if err != nil {
		return nil, err
	}

	// Fetch metadata for each message using format=full to get body text
	var objects []ObjectInfo
	for _, msg := range allMessages {
		if len(objects) >= maxKeys {
			break
		}

		full, err := g.gmail.Users.Messages.Get(g.user, msg.Id).
			Format("full").
			Context(ctx).
			Do()
		if err != nil {
			slog.WarnContext(ctx, "Failed to fetch message metadata",
				"message_id", msg.Id, "error", err,
			)
			continue
		}

		if obj, ok := messageToObjectInfo(full, bucket, startAfter); ok {
			objects = append(objects, obj)
		}
	}

	// Sort by key
	sort.Slice(objects, func(i, j int) bool {
		return objects[i].Key < objects[j].Key
	})

	result := collapseListResult(objects, prefix, delimiter, maxKeys)
	g.recordGmailOp("ListObjects", start, nil)
	return result, nil
}

// listGmailMessageIDs paginates the Gmail search for the given query and returns
// the matching message stubs, stopping once maxKeys have been collected.
func (g *GmailBackend) listGmailMessageIDs(ctx context.Context, query string, maxKeys int, start time.Time) ([]*gmail.Message, error) {
	var allMessages []*gmail.Message
	pageToken := ""

	for {
		req := g.gmail.Users.Messages.List(g.user).Q(query).MaxResults(int64(maxKeys))
		if pageToken != "" {
			req = req.PageToken(pageToken)
		}
		resp, err := req.Context(ctx).Do()
		if err != nil {
			g.recordGmailOp("ListObjects", start, err)
			return nil, fmt.Errorf("gmail list: %w", err)
		}
		allMessages = append(allMessages, resp.Messages...)

		if resp.NextPageToken == "" || len(allMessages) >= maxKeys {
			break
		}
		pageToken = resp.NextPageToken
	}

	return allMessages, nil
}

// messageToObjectInfo converts a fully-fetched Gmail message into an ObjectInfo.
// It returns ok=false when the message is not a listable object: a non-object
// subject, a chunk component email, or a key at or before startAfter.
func messageToObjectInfo(full *gmail.Message, bucket, startAfter string) (ObjectInfo, bool) {
	key := extractKeyFromSubject(headerValue(full.Payload.Headers, "Subject"), bucket)
	if key == "" {
		return ObjectInfo{}, false
	}

	// Skip chunk emails that made it past the Gmail search filter
	if strings.Contains(key, "#chunk-") {
		return ObjectInfo{}, false
	}

	// Filter by startAfter
	if startAfter != "" && key <= startAfter {
		return ObjectInfo{}, false
	}

	obj := ObjectInfo{
		Key:          key,
		Size:         full.SizeEstimate,
		LastModified: time.UnixMilli(full.InternalDate),
	}
	applyBodyMetadata(&obj, full.Payload)
	return obj, true
}

// headerValue returns the value of the first header matching name, or "" if no
// such header is present.
func headerValue(headers []*gmail.MessagePartHeader, name string) string {
	for _, h := range headers {
		if h.Name == name {
			return h.Value
		}
	}
	return ""
}

// applyBodyMetadata parses the message body text and overlays the ETag and an
// accurate size (full size for chunked objects) onto obj. The SizeEstimate-based
// values already on obj are left in place when no parseable metadata is found.
func applyBodyMetadata(obj *ObjectInfo, payload *gmail.MessagePart) {
	bodyText := extractBodyText(payload)
	if bodyText == "" {
		return
	}
	meta, err := parseMetadataOnly(bodyText)
	if err != nil {
		return
	}

	obj.ETag = meta.ETag
	if meta.Chunked && meta.TotalSize > 0 {
		obj.Size = meta.TotalSize
	} else {
		obj.Size = meta.Size
	}
}

// collapseListResult applies delimiter-based common-prefix grouping and maxKeys
// truncation to a key-sorted object slice, producing a ListObjectsV2 response.
func collapseListResult(objects []ObjectInfo, prefix, delimiter string, maxKeys int) *ListObjectsResult {
	result := &ListObjectsResult{}

	// Apply delimiter for common prefixes
	if delimiter != "" {
		seen := make(map[string]bool)
		var filtered []ObjectInfo
		for _, obj := range objects {
			rel := strings.TrimPrefix(obj.Key, prefix)
			idx := strings.Index(rel, delimiter)
			if idx >= 0 {
				cp := prefix + rel[:idx+len(delimiter)]
				if !seen[cp] {
					seen[cp] = true
					result.CommonPrefixes = append(result.CommonPrefixes, cp)
				}
			} else {
				filtered = append(filtered, obj)
			}
		}
		objects = filtered
	}

	// Truncate to maxKeys
	if len(objects) > maxKeys {
		objects = objects[:maxKeys]
		result.IsTruncated = true
		result.NextStartAfter = objects[maxKeys-1].Key
	}

	result.Contents = objects
	return result
}

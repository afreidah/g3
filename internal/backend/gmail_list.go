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

	labels, err := g.svc.Users.Labels.List(g.user).Context(ctx).Do()
	if err != nil {
		g.recordOp("ListBuckets", start, err)
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

	g.recordOp("ListBuckets", start, nil)
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
	labels, err := g.svc.Users.Labels.List(g.user).Context(ctx).Do()
	if err != nil {
		g.recordOp("CreateBucket", start, err)
		return fmt.Errorf("list labels: %w", err)
	}
	for _, l := range labels.Labels {
		if l.Name == name {
			g.recordOp("CreateBucket", start, nil)
			return ErrBucketExists
		}
	}

	label := &gmail.Label{
		Name:                  name,
		LabelListVisibility:   "labelHide",
		MessageListVisibility: "hide",
	}
	created, err := g.svc.Users.Labels.Create(g.user, label).Context(ctx).Do()
	if err != nil {
		g.recordOp("CreateBucket", start, err)
		return fmt.Errorf("create label: %w", err)
	}

	g.labelMu.Lock()
	g.labelCache[name] = created.Id
	g.labelMu.Unlock()

	slog.InfoContext(ctx, "Bucket created", "bucket", bucket, "label_id", created.Id)
	g.recordOp("CreateBucket", start, nil)
	return nil
}

// -------------------------------------------------------------------------
// OBJECT LISTING
// -------------------------------------------------------------------------

// ListObjects searches Gmail for emails matching the given bucket and prefix,
// returning results in S3 ListObjectsV2 format with delimiter and pagination
// support.
func (g *GmailBackend) ListObjects(ctx context.Context, bucket, prefix, delimiter, startAfter string, maxKeys int) (*ListObjectsResult, error) {
	start := time.Now()
	ctx, span := telemetry.StartClientSpan(ctx, "Gmail.ListMessages",
		telemetry.GmailAttributes("ListObjects", bucket, prefix)...,
	)
	defer span.End()

	if maxKeys <= 0 {
		maxKeys = 1000
	}

	query := buildListQuery(g.labelPrefix, bucket, prefix)
	var allMessages []*gmail.Message
	pageToken := ""

	// Paginate through Gmail results
	for {
		req := g.svc.Users.Messages.List(g.user).Q(query).MaxResults(int64(maxKeys))
		if pageToken != "" {
			req = req.PageToken(pageToken)
		}
		resp, err := req.Context(ctx).Do()
		if err != nil {
			g.recordOp("ListObjects", start, err)
			return nil, fmt.Errorf("gmail list: %w", err)
		}
		allMessages = append(allMessages, resp.Messages...)

		if resp.NextPageToken == "" || len(allMessages) >= maxKeys {
			break
		}
		pageToken = resp.NextPageToken
	}

	// Fetch metadata for each message
	var objects []ObjectInfo
	for _, msg := range allMessages {
		if len(objects) >= maxKeys {
			break
		}

		full, err := g.svc.Users.Messages.Get(g.user, msg.Id).
			Format("metadata").
			MetadataHeaders("Subject").
			Context(ctx).
			Do()
		if err != nil {
			slog.WarnContext(ctx, "Failed to fetch message metadata",
				"message_id", msg.Id, "error", err,
			)
			continue
		}

		subject := ""
		for _, h := range full.Payload.Headers {
			if h.Name == "Subject" {
				subject = h.Value
				break
			}
		}

		key := extractKeyFromSubject(subject, bucket)
		if key == "" {
			continue
		}

		// Filter by startAfter
		if startAfter != "" && key <= startAfter {
			continue
		}

		objects = append(objects, ObjectInfo{
			Key:          key,
			Size:         full.SizeEstimate,
			LastModified: time.UnixMilli(full.InternalDate),
		})
	}

	// Sort by key
	sort.Slice(objects, func(i, j int) bool {
		return objects[i].Key < objects[j].Key
	})

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
	g.recordOp("ListObjects", start, nil)
	return result, nil
}

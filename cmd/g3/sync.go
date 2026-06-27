// -------------------------------------------------------------------------------
// Sync - Rebuild SQLite Index from Gmail
//
// Author: Alex Freidah
//
// Scans all Gmail emails under the configured label prefix and populates
// the local SQLite metadata index. Use this to recover the index after data
// loss or to index objects written before the SQLite layer was added.
// -------------------------------------------------------------------------------

package main

import (
	"context"
	"encoding/base64"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/afreidah/g3/internal/backend"
	"github.com/afreidah/g3/internal/config"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"
)

// bucketLabel pairs a discovered bucket name with its Gmail label ID.
type bucketLabel struct {
	name    string
	labelID string
}

// runSync rebuilds the metadata index by scanning Gmail.
func runSync() {
	configPath := flag.String("config", "config.yaml", "path to config file")
	_ = flag.CommandLine.Parse(os.Args[2:])

	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		os.Exit(1)
	}

	// Initialize logging
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: config.ParseLogLevel(cfg.Server.LogLevel),
	})))

	ctx := context.Background()

	metadataStore, closeStore, err := initMetadataStore(ctx, &cfg.Database)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to initialize metadata store", "error", err)
		os.Exit(1)
	}
	defer closeStore()

	gmailSvc, err := newGmailReadClient(ctx, &cfg.Gmail)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to create Gmail service", "error", err)
		os.Exit(1)
	}

	prefix := cfg.Gmail.LabelPrefix + "/"
	user := cfg.Gmail.User

	slog.InfoContext(ctx, "Scanning labels for buckets", "prefix", prefix)
	buckets, err := discoverBuckets(ctx, gmailSvc, metadataStore, user, prefix)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to list labels", "error", err)
		os.Exit(1)
	}

	totalObjects := 0
	for _, bl := range buckets {
		totalObjects += syncBucket(ctx, gmailSvc, metadataStore, user, cfg.Gmail.LabelPrefix, bl)
	}

	slog.InfoContext(ctx, "Sync complete",
		"buckets", len(buckets),
		"objects", totalObjects,
	)
}

// newGmailReadClient builds a read-only Gmail service from the configured OAuth
// credentials.
func newGmailReadClient(ctx context.Context, cfg *config.GmailConfig) (*gmail.Service, error) {
	oauthCfg := &oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		Endpoint:     google.Endpoint,
		Scopes:       []string{gmail.GmailReadonlyScope, drive.DriveFileScope},
	}
	tok := &oauth2.Token{RefreshToken: cfg.RefreshToken}
	client := oauthCfg.Client(ctx, tok)
	return gmail.NewService(ctx, option.WithHTTPClient(client))
}

// discoverBuckets lists Gmail labels under the prefix, records each as a bucket
// in the metadata store, and returns the discovered bucket labels.
func discoverBuckets(ctx context.Context, gmailSvc *gmail.Service, metadataStore backend.MetadataStore, user, prefix string) ([]bucketLabel, error) {
	labels, err := gmailSvc.Users.Labels.List(user).Context(ctx).Do()
	if err != nil {
		return nil, err
	}

	var buckets []bucketLabel
	for _, l := range labels.Labels {
		name, ok := bucketNameFromLabel(l.Name, prefix)
		if !ok {
			continue
		}
		buckets = append(buckets, bucketLabel{name: name, labelID: l.Id})
		_ = metadataStore.PutBucket(ctx, &backend.BucketRecord{
			Name:      name,
			LabelID:   l.Id,
			CreatedAt: time.Now().UTC(),
		})
		slog.InfoContext(ctx, "Bucket indexed", "bucket", name, "label_id", l.Id)
	}
	return buckets, nil
}

// bucketNameFromLabel returns the bucket name for a label under the prefix, or
// ok=false if the label is not a top-level bucket label.
func bucketNameFromLabel(labelName, prefix string) (string, bool) {
	if !strings.HasPrefix(labelName, prefix) {
		return "", false
	}
	name := strings.TrimPrefix(labelName, prefix)
	if name == "" || strings.Contains(name, "/") {
		return "", false
	}
	return name, true
}

// syncBucket scans every object email in a bucket and indexes it, returning the
// number of objects indexed.
func syncBucket(ctx context.Context, gmailSvc *gmail.Service, metadataStore backend.MetadataStore, user, labelPrefix string, bl bucketLabel) int {
	escapedLabel := strings.ReplaceAll(labelPrefix+"/"+bl.name, "/", "-")
	query := fmt.Sprintf("label:%s subject:(s3://) -subject:(#chunk-)", escapedLabel)

	slog.InfoContext(ctx, "Scanning bucket", "bucket", bl.name)

	count := 0
	pageToken := ""
	for {
		req := gmailSvc.Users.Messages.List(user).Q(query).MaxResults(100)
		if pageToken != "" {
			req = req.PageToken(pageToken)
		}
		resp, err := req.Context(ctx).Do()
		if err != nil {
			slog.ErrorContext(ctx, "Failed to list messages", "bucket", bl.name, "error", err)
			break
		}

		for _, msgRef := range resp.Messages {
			if indexMessage(ctx, gmailSvc, metadataStore, user, bl.name, msgRef.Id) {
				count++
			}
		}

		if resp.NextPageToken == "" {
			break
		}
		pageToken = resp.NextPageToken
	}
	return count
}

// indexMessage fetches one object email, parses its metadata, and writes the
// record to the store. Returns true when an object was indexed.
func indexMessage(ctx context.Context, gmailSvc *gmail.Service, metadataStore backend.MetadataStore, user, bucket, msgID string) bool {
	msg, err := gmailSvc.Users.Messages.Get(user, msgID).
		Format("full").
		Context(ctx).
		Do()
	if err != nil {
		slog.WarnContext(ctx, "Failed to fetch message", "id", msgID, "error", err)
		return false
	}

	keyPrefix := "s3://" + bucket + "/"
	subject := gmailHeaderValue(msg.Payload.Headers, "Subject")
	if !strings.HasPrefix(subject, keyPrefix) {
		return false
	}
	key := strings.TrimPrefix(subject, keyPrefix)

	// Skip chunk emails
	if strings.Contains(key, "#chunk-") {
		return false
	}

	bodyText := extractBodyFromPayload(msg.Payload)
	if bodyText == "" {
		slog.WarnContext(ctx, "No body text", "id", msgID, "key", key)
		return false
	}

	meta, err := backend.ParseMetadataForSync(bodyText)
	if err != nil {
		slog.WarnContext(ctx, "Failed to parse metadata", "id", msgID, "key", key, "error", err)
		return false
	}

	_ = metadataStore.PutObject(ctx, &backend.ObjectRecord{
		Bucket:      bucket,
		Key:         key,
		GmailMsgID:  msgID,
		DriveFileID: meta.DriveFileID,
		ETag:        meta.ETag,
		Size:        meta.Size,
		ContentType: meta.ContentType,
		CreatedAt:   meta.CreatedAt,
		Metadata:    meta.Metadata,
	})
	return true
}

// gmailHeaderValue returns the value of the first header matching name, or "".
func gmailHeaderValue(headers []*gmail.MessagePartHeader, name string) string {
	for _, h := range headers {
		if h.Name == name {
			return h.Value
		}
	}
	return ""
}

// extractBodyFromPayload pulls plain text body from a Gmail message payload.
func extractBodyFromPayload(payload *gmail.MessagePart) string {
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

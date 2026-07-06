// -------------------------------------------------------------------------------
// g3 - Sync Subcommand
//
// Author: Alex Freidah
//
// Rebuilds the metadata index by scanning Gmail. Run loads configuration, opens
// the metadata store, and walks every object email under the configured label
// prefix, indexing each. The scan/index helpers take a gmailAPI interface so
// they are unit-testable without a live Gmail connection.
// -------------------------------------------------------------------------------

package synccmd

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/afreidah/g3/internal/backend"
	"github.com/afreidah/g3/internal/cli"
	"github.com/afreidah/g3/internal/config"

	"google.golang.org/api/gmail/v1"
)

// bucketLabel pairs a discovered bucket name with its Gmail label ID.
type bucketLabel struct {
	name    string
	labelID string
}

// Run rebuilds the metadata index from Gmail and returns the process exit code.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("sync", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", cli.DefaultConfigPath, "path to config file")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	cfg, err := loadConfig(*configPath)
	if err != nil {
		fmt.Fprintf(stderr, "failed to load config: %v\n", err)
		return 1
	}

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: config.ParseLogLevel(cfg.Server.LogLevel),
	})))

	metadataStore, closeStore, err := openStore(ctx, &cfg.Database)
	if err != nil {
		fmt.Fprintf(stderr, "failed to initialize metadata store: %v\n", err)
		return 1
	}
	defer closeStore()

	client, err := newGmailClient(ctx, &cfg.Gmail)
	if err != nil {
		fmt.Fprintf(stderr, "failed to create Gmail service: %v\n", err)
		return 1
	}

	prefix := cfg.Gmail.LabelPrefix + "/"
	slog.InfoContext(ctx, "Scanning labels for buckets", "prefix", prefix)
	buckets, err := discoverBuckets(ctx, client, metadataStore, prefix)
	if err != nil {
		fmt.Fprintf(stderr, "failed to list labels: %v\n", err)
		return 1
	}

	total := 0
	for _, bl := range buckets {
		total += syncBucket(ctx, client, metadataStore, cfg.Gmail.LabelPrefix, bl)
	}

	fmt.Fprintf(stdout, "sync complete: %d bucket(s), %d object(s)\n", len(buckets), total)
	return 0
}

// discoverBuckets lists Gmail labels under the prefix, records each as a bucket
// in the metadata store, and returns the discovered bucket labels.
func discoverBuckets(ctx context.Context, client gmailAPI, metadataStore backend.MetadataStore, prefix string) ([]bucketLabel, error) {
	labels, err := client.ListLabels(ctx)
	if err != nil {
		return nil, err
	}

	var buckets []bucketLabel
	for _, l := range labels {
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
func syncBucket(ctx context.Context, client gmailAPI, metadataStore backend.MetadataStore, labelPrefix string, bl bucketLabel) int {
	escapedLabel := strings.ReplaceAll(labelPrefix+"/"+bl.name, "/", "-")
	query := fmt.Sprintf("label:%s subject:(s3://) -subject:(#chunk-)", escapedLabel)

	slog.InfoContext(ctx, "Scanning bucket", "bucket", bl.name)

	count := 0
	pageToken := ""
	for {
		msgs, next, err := client.ListMessages(ctx, query, pageToken, 100)
		if err != nil {
			slog.ErrorContext(ctx, "Failed to list messages", "bucket", bl.name, "error", err)
			break
		}

		for _, msgRef := range msgs {
			if indexMessage(ctx, client, metadataStore, bl.name, msgRef.Id) {
				count++
			}
		}

		if next == "" {
			break
		}
		pageToken = next
	}
	return count
}

// indexMessage fetches one object email, parses its metadata, and writes the
// record to the store. Returns true when an object was indexed.
func indexMessage(ctx context.Context, client gmailAPI, metadataStore backend.MetadataStore, bucket, msgID string) bool {
	msg, err := client.GetMessage(ctx, msgID, "full")
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

	bodyText := backend.ExtractBodyText(msg.Payload)
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

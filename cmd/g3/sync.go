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
	"github.com/afreidah/g3/internal/store"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"
)

// runSync rebuilds the SQLite metadata index by scanning Gmail.
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

	// Open metadata store
	var metadataStore backend.MetadataStore
	switch cfg.Database.Driver {
	case "postgres":
		pgStore, pgErr := store.NewPostgres(ctx, &cfg.Database)
		if pgErr != nil {
			slog.ErrorContext(ctx, "Failed to initialize PostgreSQL store", "error", pgErr)
			os.Exit(1)
		}
		defer func() { _ = pgStore.Close() }()
		metadataStore = pgStore
	default:
		sqliteStore, sqliteErr := store.NewSQLite(cfg.Database.Path)
		if sqliteErr != nil {
			slog.ErrorContext(ctx, "Failed to initialize SQLite store", "error", sqliteErr)
			os.Exit(1)
		}
		defer func() { _ = sqliteStore.Close() }()
		metadataStore = sqliteStore
	}

	// Create Gmail client
	oauthCfg := &oauth2.Config{
		ClientID:     cfg.Gmail.ClientID,
		ClientSecret: cfg.Gmail.ClientSecret,
		Endpoint:     google.Endpoint,
		Scopes:       []string{gmail.GmailReadonlyScope, drive.DriveFileScope},
	}
	tok := &oauth2.Token{RefreshToken: cfg.Gmail.RefreshToken}
	client := oauthCfg.Client(ctx, tok)

	gmailSvc, err := gmail.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		slog.ErrorContext(ctx, "Failed to create Gmail service", "error", err)
		os.Exit(1)
	}

	prefix := cfg.Gmail.LabelPrefix + "/"
	user := cfg.Gmail.User

	// Discover buckets from labels
	slog.InfoContext(ctx, "Scanning labels for buckets", "prefix", prefix)
	labels, err := gmailSvc.Users.Labels.List(user).Context(ctx).Do()
	if err != nil {
		slog.ErrorContext(ctx, "Failed to list labels", "error", err)
		os.Exit(1)
	}

	var bucketLabels []struct {
		name    string
		labelID string
	}
	for _, l := range labels.Labels {
		if strings.HasPrefix(l.Name, prefix) {
			name := strings.TrimPrefix(l.Name, prefix)
			if name != "" && !strings.Contains(name, "/") {
				bucketLabels = append(bucketLabels, struct {
					name    string
					labelID string
				}{name, l.Id})
				_ = metadataStore.PutBucket(ctx, &backend.BucketRecord{
					Name:      name,
					LabelID:   l.Id,
					CreatedAt: time.Now().UTC(),
				})
				slog.InfoContext(ctx, "Bucket indexed", "bucket", name, "label_id", l.Id)
			}
		}
	}

	// Scan objects in each bucket
	totalObjects := 0
	for _, bl := range bucketLabels {
		label := cfg.Gmail.LabelPrefix + "/" + bl.name
		escapedLabel := strings.ReplaceAll(label, "/", "-")
		query := fmt.Sprintf("label:%s subject:(s3://) -subject:(#chunk-)", escapedLabel)

		slog.InfoContext(ctx, "Scanning bucket", "bucket", bl.name)

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
				msg, err := gmailSvc.Users.Messages.Get(user, msgRef.Id).
					Format("full").
					Context(ctx).
					Do()
				if err != nil {
					slog.WarnContext(ctx, "Failed to fetch message", "id", msgRef.Id, "error", err)
					continue
				}

				// Extract subject
				subject := ""
				for _, h := range msg.Payload.Headers {
					if h.Name == "Subject" {
						subject = h.Value
						break
					}
				}

				keyPrefix := "s3://" + bl.name + "/"
				if !strings.HasPrefix(subject, keyPrefix) {
					continue
				}
				key := strings.TrimPrefix(subject, keyPrefix)

				// Skip chunk emails
				if strings.Contains(key, "#chunk-") {
					continue
				}

				// Extract body text for metadata
				bodyText := extractBodyFromPayload(msg.Payload)
				if bodyText == "" {
					slog.WarnContext(ctx, "No body text", "id", msgRef.Id, "key", key)
					continue
				}

				meta, err := backend.ParseMetadataForSync(bodyText)
				if err != nil {
					slog.WarnContext(ctx, "Failed to parse metadata", "id", msgRef.Id, "key", key, "error", err)
					continue
				}

				_ = metadataStore.PutObject(ctx, &backend.ObjectRecord{
					Bucket:      bl.name,
					Key:         key,
					GmailMsgID:  msgRef.Id,
					DriveFileID: meta.DriveFileID,
					ETag:        meta.ETag,
					Size:        meta.Size,
					ContentType: meta.ContentType,
					CreatedAt:   meta.CreatedAt,
					Metadata:    meta.Metadata,
				})
				totalObjects++
			}

			if resp.NextPageToken == "" {
				break
			}
			pageToken = resp.NextPageToken
		}
	}

	slog.InfoContext(ctx, "Sync complete",
		"buckets", len(bucketLabels),
		"objects", totalObjects,
	)
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

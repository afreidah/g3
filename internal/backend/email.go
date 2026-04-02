// -------------------------------------------------------------------------------
// Email Construction - Message Building for Gmail Storage
//
// Author: Alex Freidah
//
// Builds emails that encode S3 objects for Gmail storage. Object metadata is
// stored as JSON in the plain text body (visible in Gmail UI and fetchable
// without downloading attachments). Object data is carried as a binary
// attachment. This separation enables fast metadata-only reads via the Gmail
// API's format=full parameter.
// -------------------------------------------------------------------------------

package backend

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/textproto"
	"strings"
	"time"
)

// -------------------------------------------------------------------------
// METADATA
// -------------------------------------------------------------------------

// objectMetadata is stored as JSON in the email body. It records all
// information needed to reconstruct S3 response headers without reading the
// attachment.
type objectMetadata struct {
	ContentType string            `json:"content_type"`
	ETag        string            `json:"etag"`
	Size        int64             `json:"size"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	DriveFileID string            `json:"drive_file_id,omitempty"`
	Chunked     bool              `json:"chunked,omitempty"`
	ChunkCount  int               `json:"chunk_count,omitempty"`
	TotalSize   int64             `json:"total_size,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
}

// -------------------------------------------------------------------------
// EMAIL BUILDING
// -------------------------------------------------------------------------

// buildObjectEmail creates a raw RFC 2822 message. When data is nil (manifest
// emails), the message is plain text with JSON metadata as the body. When data
// is present, the message is multipart/mixed with the JSON body and a binary
// attachment.
func buildObjectEmail(subject string, meta *objectMetadata, data []byte) ([]byte, error) {
	metaJSON, err := json.Marshal(meta)
	if err != nil {
		return nil, fmt.Errorf("marshal metadata: %w", err)
	}

	if len(data) == 0 {
		return buildPlainEmail(subject, metaJSON), nil
	}
	return buildMultipartEmail(subject, metaJSON, data)
}

// buildPlainEmail creates a simple text/plain email with JSON metadata body.
func buildPlainEmail(subject string, metaJSON []byte) []byte {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "From: me\r\n")
	fmt.Fprintf(&buf, "To: me\r\n")
	fmt.Fprintf(&buf, "Subject: %s\r\n", subject)
	fmt.Fprintf(&buf, "MIME-Version: 1.0\r\n")
	fmt.Fprintf(&buf, "Content-Type: text/plain; charset=utf-8\r\n")
	fmt.Fprintf(&buf, "\r\n")
	buf.Write(metaJSON)
	return buf.Bytes()
}

// buildMultipartEmail creates a multipart/mixed email with a JSON text body
// and a binary attachment.
func buildMultipartEmail(subject string, metaJSON, data []byte) ([]byte, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	boundary := writer.Boundary()

	var header bytes.Buffer
	fmt.Fprintf(&header, "From: me\r\n")
	fmt.Fprintf(&header, "To: me\r\n")
	fmt.Fprintf(&header, "Subject: %s\r\n", subject)
	fmt.Fprintf(&header, "MIME-Version: 1.0\r\n")
	fmt.Fprintf(&header, "Content-Type: multipart/mixed; boundary=%q\r\n", boundary)
	fmt.Fprintf(&header, "\r\n")

	// JSON metadata as text/plain body
	metaHeader := make(textproto.MIMEHeader)
	metaHeader.Set("Content-Type", "text/plain; charset=utf-8")
	metaPart, err := writer.CreatePart(metaHeader)
	if err != nil {
		return nil, fmt.Errorf("create metadata part: %w", err)
	}
	if _, err := metaPart.Write(metaJSON); err != nil {
		return nil, fmt.Errorf("write metadata: %w", err)
	}

	// Object data attachment
	attHeader := make(textproto.MIMEHeader)
	attHeader.Set("Content-Type", "application/octet-stream")
	attHeader.Set("Content-Disposition", `attachment; filename="object.bin"`)
	attHeader.Set("Content-Transfer-Encoding", "base64")
	attPart, err := writer.CreatePart(attHeader)
	if err != nil {
		return nil, fmt.Errorf("create attachment part: %w", err)
	}

	encoder := base64.NewEncoder(base64.StdEncoding, attPart)
	if _, err := encoder.Write(data); err != nil {
		return nil, fmt.Errorf("encode attachment: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return nil, fmt.Errorf("close encoder: %w", err)
	}

	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("close multipart: %w", err)
	}

	result := make([]byte, 0, header.Len()+body.Len())
	result = append(result, header.Bytes()...)
	result = append(result, body.Bytes()...)
	return result, nil
}

// -------------------------------------------------------------------------
// EMAIL PARSING
// -------------------------------------------------------------------------

// parseMetadataOnly extracts object metadata from the email body without
// downloading attachment data. Works with Gmail API format=full responses
// where the body text is available but attachments are not.
func parseMetadataOnly(bodyText string) (*objectMetadata, error) {
	meta := &objectMetadata{}
	if err := json.Unmarshal([]byte(bodyText), meta); err != nil {
		return nil, fmt.Errorf("unmarshal metadata: %w", err)
	}
	return meta, nil
}

// SyncMetadata holds parsed metadata fields needed by the sync command.
type SyncMetadata struct {
	ContentType string
	ETag        string
	Size        int64
	DriveFileID string
	CreatedAt   time.Time
	Metadata    map[string]string
}

// ParseMetadataForSync parses the JSON email body and returns metadata
// fields for populating the SQLite index during sync.
func ParseMetadataForSync(bodyText string) (*SyncMetadata, error) {
	meta, err := parseMetadataOnly(bodyText)
	if err != nil {
		return nil, err
	}
	return &SyncMetadata{
		ContentType: meta.ContentType,
		ETag:        meta.ETag,
		Size:        meta.Size,
		DriveFileID: meta.DriveFileID,
		CreatedAt:   meta.CreatedAt,
		Metadata:    meta.Metadata,
	}, nil
}

// parseObjectEmail extracts metadata and attachment data from a raw MIME
// message. Used for GetObject where the full object data is needed.
func parseObjectEmail(raw []byte) (*objectMetadata, []byte, error) {
	// Split headers from body
	headerEnd := bytes.Index(raw, []byte("\r\n\r\n"))
	if headerEnd == -1 {
		headerEnd = bytes.Index(raw, []byte("\n\n"))
		if headerEnd == -1 {
			return nil, nil, fmt.Errorf("no header/body separator found")
		}
	}

	// Determine content type
	headerSection := string(raw[:headerEnd])
	contentType := ""
	for _, line := range strings.Split(headerSection, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToLower(line), "content-type:") {
			contentType = strings.TrimSpace(strings.TrimPrefix(line, "Content-Type: "))
			break
		}
	}

	bodyStart := headerEnd + 4
	if raw[headerEnd] == '\n' {
		bodyStart = headerEnd + 2
	}
	bodyBytes := raw[bodyStart:]

	// Plain text email (manifest or metadata-only)
	if strings.HasPrefix(contentType, "text/plain") {
		meta, err := parseMetadataOnly(string(bodyBytes))
		return meta, nil, err
	}

	// Multipart email
	_, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return nil, nil, fmt.Errorf("parse content type: %w", err)
	}
	boundary := params["boundary"]
	if boundary == "" {
		return nil, nil, fmt.Errorf("no boundary in content type")
	}

	reader := multipart.NewReader(bytes.NewReader(bodyBytes), boundary)

	var meta *objectMetadata
	var attachmentData []byte

	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, fmt.Errorf("read part: %w", err)
		}

		ct := part.Header.Get("Content-Type")
		if strings.HasPrefix(ct, "text/plain") && meta == nil {
			data, err := io.ReadAll(part)
			if err != nil {
				return nil, nil, fmt.Errorf("read metadata: %w", err)
			}
			meta = &objectMetadata{}
			if err := json.Unmarshal(data, meta); err != nil {
				return nil, nil, fmt.Errorf("unmarshal metadata: %w", err)
			}
		} else if part.Header.Get("Content-Disposition") != "" {
			encoding := part.Header.Get("Content-Transfer-Encoding")
			var r io.Reader = part
			if strings.EqualFold(encoding, "base64") {
				r = base64.NewDecoder(base64.StdEncoding, r)
			}
			data, err := io.ReadAll(r)
			if err != nil {
				return nil, nil, fmt.Errorf("read attachment: %w", err)
			}
			attachmentData = data
		}
		_ = part.Close()
	}

	if meta == nil {
		return nil, nil, fmt.Errorf("no metadata found in email")
	}

	return meta, attachmentData, nil
}

// -------------------------------------------------------------------------
// SUBJECT FORMATTING
// -------------------------------------------------------------------------

// objectSubject returns the email subject line for an object key.
func objectSubject(bucket, key string) string {
	return "s3://" + bucket + "/" + key
}

// chunkSubject returns the email subject line for a chunk of a large object.
func chunkSubject(bucket, key string, index int) string {
	return fmt.Sprintf("s3://%s/%s#chunk-%03d", bucket, key, index)
}

// labelName returns the Gmail label name for a bucket.
func labelName(prefix, bucket string) string {
	return prefix + "/" + bucket
}

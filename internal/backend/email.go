// -------------------------------------------------------------------------------
// Email Construction - MIME Message Building for Gmail Storage
//
// Author: Alex Freidah
//
// Builds RFC 2045 MIME multipart messages that encode S3 objects as Gmail
// emails. The email body contains JSON metadata (content type, ETag, size,
// user metadata) and the attachment carries the object data. Provides
// parsing functions to extract metadata and attachment data from fetched
// messages.
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

// objectMetadata is stored as JSON in the email body part. It records all
// information needed to reconstruct S3 response headers without reading the
// attachment.
type objectMetadata struct {
	ContentType string            `json:"content_type"`
	ETag        string            `json:"etag"`
	Size        int64             `json:"size"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	Chunked     bool              `json:"chunked,omitempty"`
	ChunkCount  int               `json:"chunk_count,omitempty"`
	TotalSize   int64             `json:"total_size,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
}

// -------------------------------------------------------------------------
// EMAIL BUILDING
// -------------------------------------------------------------------------

// buildObjectEmail creates a raw RFC 2822 message with a multipart/mixed body.
// The first part is JSON metadata, the second is the object data attachment.
func buildObjectEmail(subject string, meta *objectMetadata, data []byte) ([]byte, error) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	boundary := writer.Boundary()

	// RFC 2822 headers
	var header bytes.Buffer
	fmt.Fprintf(&header, "From: me\r\n")
	fmt.Fprintf(&header, "To: me\r\n")
	fmt.Fprintf(&header, "Subject: %s\r\n", subject)
	fmt.Fprintf(&header, "MIME-Version: 1.0\r\n")
	fmt.Fprintf(&header, "Content-Type: multipart/mixed; boundary=%q\r\n", boundary)
	fmt.Fprintf(&header, "\r\n")

	// JSON metadata part
	metaJSON, err := json.Marshal(meta)
	if err != nil {
		return nil, fmt.Errorf("marshal metadata: %w", err)
	}

	metaHeader := make(textproto.MIMEHeader)
	metaHeader.Set("Content-Type", "application/json; charset=utf-8")
	metaPart, err := writer.CreatePart(metaHeader)
	if err != nil {
		return nil, fmt.Errorf("create metadata part: %w", err)
	}
	if _, err := metaPart.Write(metaJSON); err != nil {
		return nil, fmt.Errorf("write metadata: %w", err)
	}

	// Object data attachment part (only if data is present)
	if len(data) > 0 {
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
	}

	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("close multipart: %w", err)
	}

	// Combine headers + body
	result := make([]byte, 0, header.Len()+buf.Len())
	result = append(result, header.Bytes()...)
	result = append(result, buf.Bytes()...)
	return result, nil
}

// -------------------------------------------------------------------------
// EMAIL PARSING
// -------------------------------------------------------------------------

// parseObjectEmail extracts metadata and attachment data from a raw MIME
// message. Returns the metadata and the raw attachment bytes.
func parseObjectEmail(raw []byte) (*objectMetadata, []byte, error) {
	// Split headers from body at first blank line
	headerEnd := bytes.Index(raw, []byte("\r\n\r\n"))
	if headerEnd == -1 {
		headerEnd = bytes.Index(raw, []byte("\n\n"))
		if headerEnd == -1 {
			return nil, nil, fmt.Errorf("no header/body separator found")
		}
	}

	// Extract boundary from Content-Type header
	headerSection := string(raw[:headerEnd])
	boundary := ""
	for _, line := range strings.Split(headerSection, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToLower(line), "content-type:") {
			_, params, err := mime.ParseMediaType(strings.TrimPrefix(line, "Content-Type: "))
			if err == nil {
				boundary = params["boundary"]
			}
			break
		}
	}
	if boundary == "" {
		return nil, nil, fmt.Errorf("no boundary found in Content-Type header")
	}

	// Parse multipart body
	bodyStart := headerEnd + 4
	if raw[headerEnd] == '\n' {
		bodyStart = headerEnd + 2
	}
	reader := multipart.NewReader(bytes.NewReader(raw[bodyStart:]), boundary)

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
		if strings.HasPrefix(ct, "application/json") {
			// Metadata part
			data, err := io.ReadAll(part)
			if err != nil {
				return nil, nil, fmt.Errorf("read metadata: %w", err)
			}
			meta = &objectMetadata{}
			if err := json.Unmarshal(data, meta); err != nil {
				return nil, nil, fmt.Errorf("unmarshal metadata: %w", err)
			}
		} else if part.Header.Get("Content-Disposition") != "" {
			// Attachment part
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
		part.Close()
	}

	if meta == nil {
		return nil, nil, fmt.Errorf("no metadata part found in email")
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

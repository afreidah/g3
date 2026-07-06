// -------------------------------------------------------------------------------
// Gmail Backend Tests - Pure Function Unit Tests
//
// Author: Alex Freidah
//
// Tests for ExtractBodyText which parses plain text from Gmail message payloads.
// Covers nil payload, simple single-part messages, multipart messages, missing
// body, invalid base64, and non-text MIME types.
// -------------------------------------------------------------------------------

package backend

import (
	"encoding/base64"
	"testing"

	"google.golang.org/api/gmail/v1"
)

// -------------------------------------------------------------------------
// EXTRACT BODY TEXT
// -------------------------------------------------------------------------

func TestExtractBodyText_NilPayload(t *testing.T) {
	if got := ExtractBodyText(nil); got != "" {
		t.Errorf("ExtractBodyText(nil) = %q, want empty", got)
	}
}

func TestExtractBodyText_SimplePlainText(t *testing.T) {
	payload := &gmail.MessagePart{
		MimeType: "text/plain",
		Body: &gmail.MessagePartBody{
			Data: base64.URLEncoding.EncodeToString([]byte(`{"etag":"abc"}`)),
		},
	}
	got := ExtractBodyText(payload)
	if got != `{"etag":"abc"}` {
		t.Errorf("got %q, want %q", got, `{"etag":"abc"}`)
	}
}

func TestExtractBodyText_MultipartMessage(t *testing.T) {
	payload := &gmail.MessagePart{
		MimeType: "multipart/mixed",
		Parts: []*gmail.MessagePart{
			{MimeType: "text/html", Body: &gmail.MessagePartBody{Data: base64.URLEncoding.EncodeToString([]byte("<b>html</b>"))}},
			{MimeType: "text/plain", Body: &gmail.MessagePartBody{Data: base64.URLEncoding.EncodeToString([]byte("plain text body"))}},
		},
	}
	got := ExtractBodyText(payload)
	if got != "plain text body" {
		t.Errorf("got %q, want %q", got, "plain text body")
	}
}

func TestExtractBodyText_NilBody(t *testing.T) {
	payload := &gmail.MessagePart{
		MimeType: "text/plain",
		Body:     nil,
	}
	if got := ExtractBodyText(payload); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestExtractBodyText_EmptyBodyData(t *testing.T) {
	payload := &gmail.MessagePart{
		MimeType: "text/plain",
		Body:     &gmail.MessagePartBody{Data: ""},
	}
	if got := ExtractBodyText(payload); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestExtractBodyText_InvalidBase64(t *testing.T) {
	payload := &gmail.MessagePart{
		MimeType: "text/plain",
		Body:     &gmail.MessagePartBody{Data: "!!!not-valid-base64!!!"},
	}
	if got := ExtractBodyText(payload); got != "" {
		t.Errorf("got %q, want empty (invalid base64)", got)
	}
}

func TestExtractBodyText_InvalidBase64InMultipart(t *testing.T) {
	payload := &gmail.MessagePart{
		MimeType: "multipart/mixed",
		Parts: []*gmail.MessagePart{
			{MimeType: "text/plain", Body: &gmail.MessagePartBody{Data: "!!!bad!!!"}},
			{MimeType: "text/plain", Body: &gmail.MessagePartBody{Data: base64.URLEncoding.EncodeToString([]byte("fallback"))}},
		},
	}
	got := ExtractBodyText(payload)
	if got != "fallback" {
		t.Errorf("got %q, want %q (should skip invalid part)", got, "fallback")
	}
}

func TestExtractBodyText_NonTextMimeType(t *testing.T) {
	payload := &gmail.MessagePart{
		MimeType: "application/json",
		Body:     &gmail.MessagePartBody{Data: base64.URLEncoding.EncodeToString([]byte("data"))},
	}
	if got := ExtractBodyText(payload); got != "" {
		t.Errorf("got %q, want empty (non-text MIME type)", got)
	}
}

func TestExtractBodyText_MultipartNoPlainText(t *testing.T) {
	payload := &gmail.MessagePart{
		MimeType: "multipart/mixed",
		Parts: []*gmail.MessagePart{
			{MimeType: "text/html", Body: &gmail.MessagePartBody{Data: base64.URLEncoding.EncodeToString([]byte("<b>html</b>"))}},
			{MimeType: "application/octet-stream", Body: &gmail.MessagePartBody{Data: base64.URLEncoding.EncodeToString([]byte("binary"))}},
		},
	}
	if got := ExtractBodyText(payload); got != "" {
		t.Errorf("got %q, want empty (no text/plain part)", got)
	}
}

func TestExtractBodyText_EmptyParts(t *testing.T) {
	payload := &gmail.MessagePart{
		MimeType: "multipart/mixed",
		Parts:    []*gmail.MessagePart{},
	}
	if got := ExtractBodyText(payload); got != "" {
		t.Errorf("got %q, want empty (no parts)", got)
	}
}

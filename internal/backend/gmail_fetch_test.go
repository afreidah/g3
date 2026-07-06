// -------------------------------------------------------------------------------
// Gmail Fetch Tests
//
// Author: Alex Freidah
//
// Covers findMessageIDByKey, the shared message-lookup helper used by both
// fetchObject and fetchMetadataOnly, using a stubbed Gmail service backed by
// an httptest server.
// -------------------------------------------------------------------------------

package backend

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/afreidah/g3/internal/telemetry"

	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"
)

// newStubGmail builds a *gmail.Service whose HTTP calls are served by handler.
func newStubGmail(t *testing.T, handler http.HandlerFunc) *gmail.Service {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	svc, err := gmail.NewService(context.Background(),
		option.WithHTTPClient(srv.Client()),
		option.WithEndpoint(srv.URL),
	)
	if err != nil {
		t.Fatalf("gmail.NewService: %v", err)
	}
	return svc
}

func TestFindMessageIDByKey(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		wantID  string
		wantErr error // errors.Is target; nil means "no error"
	}{
		{
			name: "found",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"messages":[{"id":"msg-123"}]}`))
			},
			wantID: "msg-123",
		},
		{
			name: "not found",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"messages":[]}`))
			},
			wantErr: ErrObjectNotFound,
		},
		{
			name: "api error",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, `{"error":{"code":500,"message":"boom"}}`, http.StatusInternalServerError)
			},
			wantErr: errSearchFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := &GmailBackend{
				gmail:       newStubGmail(t, tt.handler),
				user:        "me",
				labelPrefix: "s3/",
			}
			_, span := telemetry.StartClientSpan(context.Background(), "test")
			defer span.End()

			gotID, err := g.findMessageIDByKey(context.Background(), span, "bucket", "key")

			switch {
			case tt.wantErr == nil:
				if err != nil {
					t.Fatalf("err = %v, want nil", err)
				}
				if gotID != tt.wantID {
					t.Errorf("id = %q, want %q", gotID, tt.wantID)
				}
			case errors.Is(tt.wantErr, ErrObjectNotFound):
				if !errors.Is(err, ErrObjectNotFound) {
					t.Fatalf("err = %v, want ErrObjectNotFound", err)
				}
			default: // any non-sentinel error
				if err == nil {
					t.Fatal("err = nil, want a search error")
				}
				if errors.Is(err, ErrObjectNotFound) {
					t.Errorf("err = %v, should not be ErrObjectNotFound", err)
				}
			}
		})
	}
}

// errSearchFailed is a sentinel used only to select the "api error" branch of
// the table above; findMessageIDByKey returns a wrapped, non-sentinel error.
var errSearchFailed = errors.New("search failed")

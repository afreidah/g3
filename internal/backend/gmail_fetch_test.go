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

// lookupCase describes one findMessageIDByKey scenario. wantErr, when set, is
// matched with errors.Is; anyErr expects a non-nil, non-sentinel error.
type lookupCase struct {
	name    string
	handler http.HandlerFunc
	wantID  string
	wantErr error
	anyErr  bool
}

func TestFindMessageIDByKey(t *testing.T) {
	tests := []lookupCase{
		{
			name:    "found",
			handler: jsonHandler(`{"messages":[{"id":"msg-123"}]}`),
			wantID:  "msg-123",
		},
		{
			name:    "not found",
			handler: jsonHandler(`{"messages":[]}`),
			wantErr: ErrObjectNotFound,
		},
		{
			name: "api error",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, `{"error":{"code":500,"message":"boom"}}`, http.StatusInternalServerError)
			},
			anyErr: true,
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
			assertLookup(t, tt, gotID, err)
		})
	}
}

// jsonHandler returns a handler that writes body as a JSON response.
func jsonHandler(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}
}

// assertLookup checks a findMessageIDByKey result against the expectations in
// tc, using guard clauses to keep each scenario flat.
func assertLookup(t *testing.T, tc lookupCase, gotID string, err error) {
	t.Helper()

	if tc.wantErr != nil {
		if !errors.Is(err, tc.wantErr) {
			t.Fatalf("err = %v, want %v", err, tc.wantErr)
		}
		return
	}
	if tc.anyErr {
		if err == nil {
			t.Fatal("err = nil, want a search error")
		}
		return
	}
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if gotID != tc.wantID {
		t.Errorf("id = %q, want %q", gotID, tc.wantID)
	}
}

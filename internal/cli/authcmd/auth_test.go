// -------------------------------------------------------------------------------
// g3 - Auth Subcommand Tests
//
// Author: Alex Freidah
//
// Covers the unit-testable pieces of the auth flow: flag parsing, the OAuth
// callback handler, and Run's credential validation. The live OAuth exchange
// and localhost listener are integration concerns and are not exercised here.
// -------------------------------------------------------------------------------

package authcmd

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseAuthFlags(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		var errOut bytes.Buffer
		f, ok := parseAuthFlags([]string{"-client-id", "id", "-client-secret", "sec", "-port", "9999"}, &errOut)
		if !ok {
			t.Fatalf("expected ok; stderr=%q", errOut.String())
		}
		if f.clientID != "id" || f.clientSecret != "sec" || f.port != 9999 {
			t.Errorf("parsed = %+v", f)
		}
	})
	t.Run("missing credentials", func(t *testing.T) {
		var errOut bytes.Buffer
		if _, ok := parseAuthFlags(nil, &errOut); ok {
			t.Error("expected !ok for missing credentials")
		}
	})
	t.Run("bad flag", func(t *testing.T) {
		var errOut bytes.Buffer
		if _, ok := parseAuthFlags([]string{"-nope"}, &errOut); ok {
			t.Error("expected !ok for bad flag")
		}
	})
}

func TestCallbackHandler(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		codeCh := make(chan string, 1)
		errCh := make(chan error, 1)
		h := newCallbackHandler(codeCh, errCh)

		rr := httptest.NewRecorder()
		h(rr, httptest.NewRequest(http.MethodGet, "/callback?code=abc123", nil))

		select {
		case got := <-codeCh:
			if got != "abc123" {
				t.Errorf("code = %q, want abc123", got)
			}
		default:
			t.Fatal("expected code on channel")
		}
	})
	t.Run("provider error", func(t *testing.T) {
		codeCh := make(chan string, 1)
		errCh := make(chan error, 1)
		h := newCallbackHandler(codeCh, errCh)

		rr := httptest.NewRecorder()
		h(rr, httptest.NewRequest(http.MethodGet, "/callback?error=access_denied", nil))

		select {
		case err := <-errCh:
			if err == nil {
				t.Fatal("expected error")
			}
		default:
			t.Fatal("expected error on channel")
		}
	})
	t.Run("no code no error", func(t *testing.T) {
		codeCh := make(chan string, 1)
		errCh := make(chan error, 1)
		h := newCallbackHandler(codeCh, errCh)

		rr := httptest.NewRecorder()
		h(rr, httptest.NewRequest(http.MethodGet, "/callback", nil))

		if len(errCh) != 1 {
			t.Fatal("expected error on channel")
		}
	})
}

func TestRun_MissingCredentials(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := Run(nil, &out, &errOut); code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
}

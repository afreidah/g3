// -------------------------------------------------------------------------------
// g3 - Serve Subcommand Tests
//
// Author: Alex Freidah
//
// Covers the unit-testable pieces of the serve command: the health and
// readiness handlers and mux assembly. The daemon orchestration in Run starts a
// real listener and blocks on signals, so it is an integration concern.
// -------------------------------------------------------------------------------

package servecmd

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestHealthHandler(t *testing.T) {
	rr := httptest.NewRecorder()
	healthHandler()(rr, httptest.NewRequest(http.MethodGet, "/health", nil))

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
	if rr.Body.String() != `{"status":"ok"}` {
		t.Errorf("body = %q", rr.Body.String())
	}
}

func TestReadinessHandler(t *testing.T) {
	var ready atomic.Bool
	h := readinessHandler(&ready)

	t.Run("not ready", func(t *testing.T) {
		rr := httptest.NewRecorder()
		h(rr, httptest.NewRequest(http.MethodGet, "/health/ready", nil))
		if rr.Code != http.StatusServiceUnavailable {
			t.Errorf("status = %d, want 503", rr.Code)
		}
	})

	t.Run("ready", func(t *testing.T) {
		ready.Store(true)
		rr := httptest.NewRecorder()
		h(rr, httptest.NewRequest(http.MethodGet, "/health/ready", nil))
		if rr.Code != http.StatusOK {
			t.Errorf("status = %d, want 200", rr.Code)
		}
	})
}

func TestBuildMux(t *testing.T) {
	var ready atomic.Bool
	ready.Store(true)
	s3 := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})
	mux := buildMux(s3, true, "/metrics", &ready)

	cases := map[string]int{
		"/health":       http.StatusOK,
		"/health/ready": http.StatusOK,
		"/metrics":      http.StatusOK,
		"/anything":     http.StatusTeapot, // S3 catch-all
	}
	for path, want := range cases {
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, path, nil))
		if rr.Code != want {
			t.Errorf("%s -> %d, want %d", path, rr.Code, want)
		}
	}
}

func TestBuildMux_MetricsDisabled(t *testing.T) {
	var ready atomic.Bool
	s3 := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})
	mux := buildMux(s3, false, "/metrics", &ready)

	// With metrics disabled, /metrics falls through to the S3 catch-all.
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rr.Code != http.StatusTeapot {
		t.Errorf("metrics path -> %d, want %d (catch-all)", rr.Code, http.StatusTeapot)
	}
}

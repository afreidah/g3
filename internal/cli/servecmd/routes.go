// -------------------------------------------------------------------------------
// g3 - Serve HTTP Routes
//
// Author: Alex Freidah
//
// The liveness/readiness handlers and mux assembly for the serve command,
// factored out of the daemon orchestration so they are unit-testable.
// -------------------------------------------------------------------------------

package servecmd

import (
	"io"
	"net/http"
	"sync/atomic"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// healthHandler reports liveness; it always returns 200.
func healthHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"status":"ok"}`)
	}
}

// readinessHandler reports readiness: 503 until ready is set, then 200.
func readinessHandler(ready *atomic.Bool) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if !ready.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(w, `{"status":"not ready"}`)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"status":"ready"}`)
	}
}

// buildMux assembles the HTTP routes: liveness, readiness, optional metrics,
// and the S3 API catch-all.
func buildMux(s3Handler http.Handler, metricsEnabled bool, metricsPath string, ready *atomic.Bool) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", healthHandler())
	mux.HandleFunc("/health/ready", readinessHandler(ready))
	if metricsEnabled {
		mux.Handle(metricsPath, promhttp.Handler())
	}
	mux.Handle("/", s3Handler)
	return mux
}

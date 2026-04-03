// -------------------------------------------------------------------------------
// Metrics - Prometheus Metric Definitions
//
// Author: Alex Freidah
//
// Defines all Prometheus metrics for g3 using promauto for automatic
// registration. Metrics are prefixed with g3_ and organized by concern:
// HTTP requests, backend operations, Drive/Gmail API, multipart uploads,
// cache performance, and operational state.
// -------------------------------------------------------------------------------

package telemetry

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// -------------------------------------------------------------------------
// HTTP REQUEST METRICS
// -------------------------------------------------------------------------

// RequestsTotal counts total HTTP requests by method and status code.
var RequestsTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "g3_requests_total",
		Help: "Total number of HTTP requests processed",
	},
	[]string{"method", "status_code"},
)

// RequestDuration observes HTTP request latency in seconds by method.
var RequestDuration = promauto.NewHistogramVec(
	prometheus.HistogramOpts{
		Name:    "g3_request_duration_seconds",
		Help:    "HTTP request latency in seconds",
		Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10, 30, 60},
	},
	[]string{"method"},
)

// RequestSize observes HTTP request body sizes in bytes.
var RequestSize = promauto.NewHistogramVec(
	prometheus.HistogramOpts{
		Name:    "g3_request_size_bytes",
		Help:    "HTTP request body size in bytes",
		Buckets: prometheus.ExponentialBuckets(1024, 4, 10),
	},
	[]string{"method"},
)

// ResponseSize observes HTTP response body sizes in bytes.
var ResponseSize = promauto.NewHistogramVec(
	prometheus.HistogramOpts{
		Name:    "g3_response_size_bytes",
		Help:    "HTTP response body size in bytes",
		Buckets: prometheus.ExponentialBuckets(1024, 4, 10),
	},
	[]string{"method"},
)

// InflightRequests tracks the number of requests currently being processed.
var InflightRequests = promauto.NewGaugeVec(
	prometheus.GaugeOpts{
		Name: "g3_inflight_requests",
		Help: "Number of requests currently being processed",
	},
	[]string{"method"},
)

// AuthFailuresTotal counts authentication failures.
var AuthFailuresTotal = promauto.NewCounter(
	prometheus.CounterOpts{
		Name: "g3_auth_failures_total",
		Help: "Total number of authentication failures",
	},
)

// -------------------------------------------------------------------------
// BACKEND OPERATION METRICS
// -------------------------------------------------------------------------

// BackendDuration observes backend operation latency by operation name.
var BackendDuration = promauto.NewHistogramVec(
	prometheus.HistogramOpts{
		Name:    "g3_backend_duration_seconds",
		Help:    "Backend operation latency in seconds",
		Buckets: []float64{.01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10, 30, 60, 120},
	},
	[]string{"operation"},
)

// BackendRequestsTotal counts backend operations by operation and status.
var BackendRequestsTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "g3_backend_requests_total",
		Help: "Total number of backend operations",
	},
	[]string{"operation", "status"},
)

// ObjectBytesUploaded tracks total bytes uploaded to storage.
var ObjectBytesUploaded = promauto.NewCounter(
	prometheus.CounterOpts{
		Name: "g3_object_bytes_uploaded_total",
		Help: "Total bytes uploaded to storage",
	},
)

// ObjectBytesDownloaded tracks total bytes downloaded from storage.
var ObjectBytesDownloaded = promauto.NewCounter(
	prometheus.CounterOpts{
		Name: "g3_object_bytes_downloaded_total",
		Help: "Total bytes downloaded from storage",
	},
)

// -------------------------------------------------------------------------
// GMAIL API METRICS
// -------------------------------------------------------------------------

// GmailAPIRequestsTotal counts Gmail API calls by operation and status.
var GmailAPIRequestsTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "g3_gmail_api_requests_total",
		Help: "Total number of Gmail API requests",
	},
	[]string{"operation", "status"},
)

// GmailAPIDuration observes Gmail API request latency by operation.
var GmailAPIDuration = promauto.NewHistogramVec(
	prometheus.HistogramOpts{
		Name:    "g3_gmail_api_duration_seconds",
		Help:    "Gmail API request latency in seconds",
		Buckets: []float64{.05, .1, .25, .5, 1, 2.5, 5, 10, 30},
	},
	[]string{"operation"},
)

// -------------------------------------------------------------------------
// DRIVE API METRICS
// -------------------------------------------------------------------------

// DriveAPIRequestsTotal counts Drive API calls by operation and status.
var DriveAPIRequestsTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "g3_drive_api_requests_total",
		Help: "Total number of Google Drive API requests",
	},
	[]string{"operation", "status"},
)

// DriveAPIDuration observes Drive API request latency by operation.
var DriveAPIDuration = promauto.NewHistogramVec(
	prometheus.HistogramOpts{
		Name:    "g3_drive_api_duration_seconds",
		Help:    "Google Drive API request latency in seconds",
		Buckets: []float64{.05, .1, .25, .5, 1, 2.5, 5, 10, 30, 60},
	},
	[]string{"operation"},
)

// -------------------------------------------------------------------------
// MULTIPART UPLOAD METRICS
// -------------------------------------------------------------------------

// MultipartUploadsActive tracks the current number of in-progress uploads.
var MultipartUploadsActive = promauto.NewGauge(
	prometheus.GaugeOpts{
		Name: "g3_multipart_uploads_active",
		Help: "Number of in-progress multipart uploads",
	},
)

// MultipartUploadsCreatedTotal counts multipart uploads initiated.
var MultipartUploadsCreatedTotal = promauto.NewCounter(
	prometheus.CounterOpts{
		Name: "g3_multipart_uploads_created_total",
		Help: "Total multipart uploads created",
	},
)

// MultipartUploadsCompletedTotal counts multipart uploads completed.
var MultipartUploadsCompletedTotal = promauto.NewCounter(
	prometheus.CounterOpts{
		Name: "g3_multipart_uploads_completed_total",
		Help: "Total multipart uploads completed",
	},
)

// MultipartUploadsAbortedTotal counts multipart uploads aborted.
var MultipartUploadsAbortedTotal = promauto.NewCounter(
	prometheus.CounterOpts{
		Name: "g3_multipart_uploads_aborted_total",
		Help: "Total multipart uploads aborted",
	},
)

// MultipartUploadsExpiredTotal counts multipart uploads expired by cleanup.
var MultipartUploadsExpiredTotal = promauto.NewCounter(
	prometheus.CounterOpts{
		Name: "g3_multipart_uploads_expired_total",
		Help: "Total multipart uploads expired by cleanup",
	},
)

// -------------------------------------------------------------------------
// CACHE METRICS
// -------------------------------------------------------------------------

// LabelCacheHitsTotal counts label cache hits.
var LabelCacheHitsTotal = promauto.NewCounter(
	prometheus.CounterOpts{
		Name: "g3_label_cache_hits_total",
		Help: "Total label cache hits",
	},
)

// LabelCacheMissesTotal counts label cache misses.
var LabelCacheMissesTotal = promauto.NewCounter(
	prometheus.CounterOpts{
		Name: "g3_label_cache_misses_total",
		Help: "Total label cache misses",
	},
)

// SQLiteQueriesTotal counts SQLite queries by operation.
var SQLiteQueriesTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "g3_sqlite_queries_total",
		Help: "Total SQLite queries",
	},
	[]string{"operation"},
)

// SQLiteDuration observes SQLite query latency by operation.
var SQLiteDuration = promauto.NewHistogramVec(
	prometheus.HistogramOpts{
		Name:    "g3_sqlite_duration_seconds",
		Help:    "SQLite query latency in seconds",
		Buckets: []float64{.0001, .0005, .001, .005, .01, .05, .1},
	},
	[]string{"operation"},
)

// -------------------------------------------------------------------------
// OPERATIONAL METRICS
// -------------------------------------------------------------------------

// GmailStorageBytes tracks estimated storage usage in Gmail.
var GmailStorageBytes = promauto.NewGauge(
	prometheus.GaugeOpts{
		Name: "g3_gmail_storage_bytes",
		Help: "Estimated storage used in Gmail",
	},
)

// ObjectsTotal tracks the number of objects per bucket.
var ObjectsTotal = promauto.NewGaugeVec(
	prometheus.GaugeOpts{
		Name: "g3_objects_total",
		Help: "Number of objects per bucket",
	},
	[]string{"bucket"},
)

// AuditEventsTotal counts audit events by event name.
var AuditEventsTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "g3_audit_events_total",
		Help: "Total number of audit events",
	},
	[]string{"event"},
)

// BuildInfo exposes build metadata as a constant gauge.
var BuildInfo = promauto.NewGaugeVec(
	prometheus.GaugeOpts{
		Name: "g3_build_info",
		Help: "Build information",
	},
	[]string{"version", "go_version"},
)

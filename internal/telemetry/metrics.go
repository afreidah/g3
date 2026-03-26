// -------------------------------------------------------------------------------
// Metrics - Prometheus Metric Definitions
//
// Author: Alex Freidah
//
// Defines all Prometheus metrics for g3 using promauto for automatic
// registration. Metrics are prefixed with g3_ and organized by concern:
// HTTP request metrics, Gmail API backend metrics, and operational metrics.
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
